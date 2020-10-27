package router

import (
	"errors"
	"reflect"
	"github.com/akkeris/logtrain/internal/storage"
	"github.com/akkeris/logtrain/pkg/input"
	"github.com/papertrail/remote_syslog2/syslog"
)

/*
 * Responsibilities:
 * - Determining if a drain is misbehaving and temporarily stopping traffic to it.
 *X- A single point for incoming packets from various inputs.
 *X- Manages opening one drain per destination - on-demand - based on incoming traffic and routes
 * - Manages closing drains when no input(s) have info on that source, or when routes are removed 
 * - Measuring and receiving metrics (and reporting them).
 *
 * Principals:
 * - Only create one route per application.
 * - Data sources are added to the router, it pulls (and listens) to new routes from.
 * - Add inputs to the router (anything in ./pkg/input/).
 * - Router automatically creates ./pkg/output through the drains based on needs.
 * - The router and drains have a 1-many relationship, yet tightly dependent/coupled. 
 */

type Router struct {
	datasources []storage.DataSource
	deadPacket int
	drainByEndpoint map[string]*Drain
	drainsByHost map[string][]*Drain // Used to find open connections to endpoints by hostname
	drainsFailedToConnect map[string]bool
	endpointsByHost map[string][]string // Used to find defined endpoints by hostname (but may or may not be open)
	inputs map[string]input.Input
	stickyPools bool
	maxConnections uint32
	stop chan struct{}
	reloop chan struct{}
	running bool
}

func NewRouter(datasources []storage.DataSource, stickyPools bool, maxConnections uint32) (*Router, error) {
	router := Router{
		datasources: datasources,
		deadPacket: 0,
		drainByEndpoint: make(map[string]*Drain),
		drainsByHost: make(map[string][]*Drain),
		drainsFailedToConnect: make(map[string]bool),
		endpointsByHost: make(map[string][]string),
		inputs: make(map[string]input.Input, 0),
		stickyPools: stickyPools,
		maxConnections: maxConnections,
		stop: make(chan struct{}, 1),
		reloop: make(chan struct{}, 1),
		running: false,
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
		return errors.New("Dial cannot be called twice.")
	}
	router.running = true
	// Begin listening to datasources
	for _, source := range router.datasources {
		go func(db storage.DataSource) {
			for {
				select {
				case route := <- db.AddRoute():
					router.addRoute(route)
				case route := <- db.RemoveRoute():
					router.removeRoute(route)
				case <-router.stop:
					return
				}
			}
		}(source)
	}
	go router.writeLoop()
	return nil
}

func (router *Router) Close() error {
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
	return nil
}

func (router *Router) RemoveInput(id string) error {
	delete(router.inputs, id)
	router.reloop <- struct{}{}
	return nil
}

func (router *Router) addRoute(r storage.LogRoute) {
	if endpoints, ok := router.endpointsByHost[r.Hostname]; ok {
		var found = false
		for _, endpoint := range endpoints {
			if r.Endpoint == endpoint {
				found = true
			}
		}
		if !found {
			router.endpointsByHost[r.Hostname] = append(router.endpointsByHost[r.Hostname], r.Endpoint)
		}
	} else {
		router.endpointsByHost[r.Hostname] = make([]string, 0)
		router.endpointsByHost[r.Hostname] = append(router.endpointsByHost[r.Hostname], r.Endpoint)
	}
}

func (router *Router) removeRoute(r storage.LogRoute) {
	if endpoints, ok := router.endpointsByHost[r.Hostname]; ok {
		eps := make([]string,0)
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
	}
	// TODO: this would be an opportune time to audit the drainsByHost and drainsByEndpoint
	// disconnect any that did not appear in this route refresh.
}

func (router *Router) refreshRoutes() (error) {
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
				if drains, ok := router.drainsByHost[packet.Hostname]; ok {
					for _, drain := range drains {
						select { 
						case drain.Input<-packet:
						default:
						}
					}
				} else if endpoints, ok := router.endpointsByHost[packet.Hostname]; ok {
					drains = make([]*Drain, 0)
					for _, endpoint := range endpoints {
						// Check if an existing route is already using this endpoint and get its
						// drain, if not create a new drain for this.
						if drain, ok := router.drainByEndpoint[endpoint]; ok {
							router.drainsByHost[packet.Hostname] = append(router.drainsByHost[packet.Hostname], drain)
							drains = append(drains, drain)
						} else {
							drain, err := Create(endpoint, router.maxConnections, router.stickyPools)
							if err != nil {
								router.drainsFailedToConnect[endpoint] = true
							} else {
								if err := drain.Dial(); err != nil {
									router.drainsFailedToConnect[endpoint] = true
								} else {
									router.drainByEndpoint[endpoint] = drain
									router.drainsByHost[packet.Hostname] = append(router.drainsByHost[packet.Hostname], drain)
									drains = append(drains, drain)
								}
							}
						}	
					}
					if len(drains) > 0 {
						router.drainsByHost[packet.Hostname] = drains
					}
					for _, drain := range drains {
						select { 
						case drain.Input<-packet:
						default:
						}
					}
				} else {
					router.deadPacket++
				}
			} else if chosen == 0 /* stop */ {
				return
			} else if chosen == 1 /* reloop */ {
				remaining=0
				break
			}
		}
	}
}

