package router

import (
	"errors"
	"github.com/akkeris/logtrain/internal/debug"
	"github.com/akkeris/logtrain/internal/storage"
	"github.com/akkeris/logtrain/pkg/input"
	"github.com/trevorlinton/remote_syslog2/syslog"
	"reflect"
	"sync"
)

/*
 * Responsibilities:
 * - A single point for incoming packets from various inputs.
 * - Manages opening one drain per destination - on-demand - based on incoming traffic and routes
 * - Measuring and receiving metrics (and reporting them).
 *
 * Principals:
 * - Only create one route per application.
 * - Data sources are added to the router, it pulls (and listens) to new routes from.
 * - Add inputs to the router (anything in ./pkg/input/).
 * - Router automatically creates ./pkg/output through the drains based on needs.
 * - The router and drains have a 1-many relationship, yet tightly dependent/coupled.
 */

type Metric struct {
	MaxConnections uint32
	Connections    uint32
	Pressure       float64
	Sent           uint32
	Errors         uint32
}

type Router struct {
	datasources           []storage.DataSource
	deadPacket            int
	drainByEndpoint       map[string]*Drain
	drainsFailedToConnect map[string]bool
	endpointsByHost       map[string][]string // Used to find defined endpoints by hostname (but may or may not be open)
	inputs                map[string]input.Input
	stickyPools           bool
	maxConnections        uint32
	mutex                 *sync.Mutex
	stop                  chan struct{}
	reloop                chan struct{}
	running               bool
}

func NewRouter(datasources []storage.DataSource, stickyPools bool, maxConnections uint32) (*Router, error) {
	router := Router{
		datasources:           datasources,
		deadPacket:            0,
		drainByEndpoint:       make(map[string]*Drain),
		drainsFailedToConnect: make(map[string]bool),
		endpointsByHost:       make(map[string][]string),
		inputs:                make(map[string]input.Input, 0),
		stickyPools:           stickyPools,
		maxConnections:        maxConnections,
		mutex:                 &sync.Mutex{},
		stop:                  make(chan struct{}, 1),
		reloop:                make(chan struct{}, 1),
		running:               false,
	}
	if err := router.refreshRoutes(); err != nil {
		close(router.stop)
		close(router.reloop)
		return nil, err
	}
	return &router, nil
}

func (router *Router) Dial() error {
	if router.running == true {
		return errors.New("dial cannot be called twice")
	}
	router.running = true
	// Begin listening to datasources
	for _, source := range router.datasources {
		go func(db storage.DataSource) {
			for {
				select {
				case route := <-db.AddRoute():
					debug.Debugf("[router] Received add %s->%s\n", route.Hostname, route.Endpoint)
					go router.addRoute(route)
				case route := <-db.RemoveRoute():
					debug.Debugf("[router] Received remove %s->%s\n", route.Hostname, route.Endpoint)
					go router.removeRoute(route)
				case <-router.stop:
					debug.Debugf("[router] Data source watch loop shutting down.\n")
					return
				}
			}
		}(source)
	}
	go router.writeLoop()
	return nil
}

func (router *Router) Metrics() map[string]Metric {
	router.mutex.Lock()
	defer router.mutex.Unlock()
	metrics := make(map[string]Metric, 0)
	for host, endpoints := range router.endpointsByHost {
		for _, endpoint := range endpoints {
			if drain, ok := router.drainByEndpoint[endpoint]; ok {
				metrics[host+"->"+drain.Endpoint] = Metric{
					MaxConnections: drain.MaxConnections(),
					Connections:    drain.OpenConnections(),
					Pressure:       drain.Pressure(),
					Sent:           drain.Sent(),
					Errors:         drain.Errors(),
				}
			}
		}
	}
	return metrics
}

func (router *Router) DeadPackets() int {
	return router.deadPacket
}

func (router *Router) ResetMetrics() {
	router.mutex.Lock()
	defer router.mutex.Unlock()
	for _, endpoints := range router.endpointsByHost {
		for _, endpoint := range endpoints {
			if drain, ok := router.drainByEndpoint[endpoint]; ok {
				drain.ResetMetrics()
			}
		}
	}
	router.deadPacket = 0
}

func (router *Router) Close() error {
	debug.Debugf("[router] Closing router...\n")
	router.stop <- struct{}{}
	close(router.stop)
	close(router.reloop)
	return nil
}

func (router *Router) AddInput(in input.Input, id string) error {
	if _, ok := router.inputs[id]; ok {
		return errors.New("This input id already exists.")
	}
	router.inputs[id] = in
	router.reloop <- struct{}{}
	debug.Debugf("[router] Adding input to router %s...\n", id)
	return nil
}

func (router *Router) RemoveInput(id string) error {
	if _, ok := router.inputs[id]; ok {
		delete(router.inputs, id)
		router.reloop <- struct{}{}
	}
	debug.Debugf("[router] Removing input from router %s...\n", id)
	return nil
}

func (router *Router) addRoute(r storage.LogRoute) {
	debug.Debugf("[router] addRoute called %s->%s...\n", r.Hostname, r.Endpoint)
	router.mutex.Lock()
	defer router.mutex.Unlock()
	if endpoints, ok := router.endpointsByHost[r.Hostname]; ok {
		var found = false
		for _, endpoint := range endpoints {
			if r.Endpoint == endpoint {
				found = true
			}
		}
		if !found {
			router.endpointsByHost[r.Hostname] = append(router.endpointsByHost[r.Hostname], r.Endpoint)
		} else {
			debug.Debugf("[router] addRoute called but route already exists %s->%s\n", r.Hostname, r.Endpoint)
		}
	} else {
		router.endpointsByHost[r.Hostname] = make([]string, 0)
		router.endpointsByHost[r.Hostname] = append(router.endpointsByHost[r.Hostname], r.Endpoint)
	}
}

func (router *Router) removeRoute(r storage.LogRoute) {
	debug.Debugf("[router] removeRoute called %s->%s...\n", r.Hostname, r.Endpoint)
	router.mutex.Lock()
	defer router.mutex.Unlock()
	if endpoints, ok := router.endpointsByHost[r.Hostname]; ok {
		eps := make([]string, 0)
		for _, e := range endpoints {
			if e != r.Endpoint {
				eps = append(eps, e)
			}
		}
		if len(eps) > 0 {
			router.endpointsByHost[r.Hostname] = eps
		} else {
			delete(router.endpointsByHost, r.Hostname)
		}
	} else {
		debug.Debugf("[router] Remove route was called but route didn't exist in endpointsByHost %s->%s...\n", r.Hostname, r.Endpoint)
	}
	var foundUnusedEndpoint = true
	for _, endpoints := range router.endpointsByHost {
		for _, endpoint := range endpoints {
			if endpoint == r.Endpoint {
				foundUnusedEndpoint = false
			}
		}
	}
	if drain, ok := router.drainByEndpoint[r.Endpoint]; ok && foundUnusedEndpoint {
		debug.Debugf("[router] While removing route, discovered drain with no endpoints using it, so we'll close the drain. %s->%s\n", r.Hostname, r.Endpoint)
		drain.Close()
		delete(router.drainByEndpoint, r.Endpoint)
	}
}

func (router *Router) refreshRoutes() error {
	routes := make([]storage.LogRoute, 0)
	for _, d := range router.datasources {
		rs, err := d.GetAllRoutes()
		if err != nil {
			return err
		}
		for _, r := range rs {
			router.addRoute(r)
		}
		routes = append(routes[:], rs[:]...)
	}
	for _, route := range routes {
		var found = false
		for k, v := range router.endpointsByHost {
			for _, z := range v {
				if z == route.Endpoint && k == route.Hostname {
					found = true
				}
			}
		}
		if !found {
			router.removeRoute(route)
		}
	}
	debug.Debugf("[router] refreshRoutes finished with %d routes\n", len(router.endpointsByHost))
	return nil
}

func (router *Router) writeLoop() {
	for {
		chans := make([]chan syslog.Packet, len(router.inputs))
		for _, in := range router.inputs {
			chans = append(chans, in.Packets())
		}
		inputs := make([]reflect.SelectCase, 0)
		// THIS MUST BE ENTRY 0, DO NOT MOVE.
		inputs = append(inputs, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(router.stop)})
		// THIS MUST BE ENTRY 1, DO NOT MOVE.
		inputs = append(inputs, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(router.reloop)})
		for _, ch := range chans {
			inputs = append(inputs, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)})
		}

		remaining := len(inputs)
		for remaining > 0 {
			chosen, value, ok := reflect.Select(inputs)
			if !ok {
				// The chosen channel has been closed, so zero out the channel to disable the case
				inputs[chosen].Chan = reflect.ValueOf(nil)
				remaining -= 1
				continue
			}
			if packet, ok := value.Interface().(syslog.Packet); ok {
				if endpoints, ok := router.endpointsByHost[packet.Hostname]; ok {
					for _, endpoint := range endpoints {
						if drain, ok := router.drainByEndpoint[endpoint]; ok {
							select {
							case drain.Input <- packet:
							default:
							}
						} else {
							debug.Debugf("[router] Creating new drain to %s, using it for host %s\n", endpoint, packet.Hostname)
							drain, err := Create(endpoint, router.maxConnections, router.stickyPools)
							if err != nil {
								debug.Errorf("[router] Error creating new drain to %s, for host %s: %s\n", endpoint, packet.Hostname, err.Error())
								router.drainsFailedToConnect[endpoint] = true
							} else {
								if err := drain.Dial(); err != nil {
									debug.Errorf("[router] Error dailing new drain to %s, for host %s: %s\n", endpoint, packet.Hostname, err.Error())
									router.drainsFailedToConnect[endpoint] = true
								} else {
									debug.Errorf("[router] Successfully created new drain to %s, for host %s\n", endpoint, packet.Hostname)
									router.mutex.Lock()
									router.drainByEndpoint[endpoint] = drain
									router.mutex.Unlock()
									select {
									case drain.Input <- packet:
									default:
									}
								}
							}
						}
					}
				} else {
					router.deadPacket++
				}
			} else if chosen == 0 /* stop */ {
				debug.Debugf("[router] writeLoop exiting.\n")
				return
			} else if chosen == 1 /* reloop */ {
				debug.Debugf("[router] writeLoop relooping.\n")
				remaining = 0
				break
			}
		}
	}
}
