package storage

import (
	"errors"
)

// MemoryDataSource is used by logtail and tests
type MemoryDataSource struct {
	closed bool
	add    chan LogRoute
	remove chan LogRoute
	routes []LogRoute
}

// AddRoute returns a channel that informs the listener of new routes
func (dataSource *MemoryDataSource) AddRoute() chan LogRoute {
	return dataSource.add
}

// RemoveRoute returns a channel that informs the listener of route removals
func (dataSource *MemoryDataSource) RemoveRoute() chan LogRoute {
	return dataSource.remove
}

// GetAllRoutes returns all routes
func (dataSource *MemoryDataSource) GetAllRoutes() ([]LogRoute, error) {
	return dataSource.routes, nil
}

// EmitNewRoute adds a new route and emits it by the add route channel
func (dataSource *MemoryDataSource) EmitNewRoute(route LogRoute) error {
	dataSource.routes = append(dataSource.routes, route)
	dataSource.add <- route
	return nil
}

// EmitRemoveRoute removes a route from the datasource and emits it to the remote route channel
func (dataSource *MemoryDataSource) EmitRemoveRoute(route LogRoute) error {
	newRoutes := make([]LogRoute, 0)
	for _, r := range dataSource.routes {
		if r.Endpoint != route.Endpoint || r.Hostname != r.Hostname {
			newRoutes = append(newRoutes, r)
		}
	}
	dataSource.routes = newRoutes
	dataSource.remove <- route
	return nil
}

// Writable indicates if emitting new add/remove routes can be called safely, some datasources
// are read only and cannot be written to.
func (dataSource *MemoryDataSource) Writable() bool {
	return true
}

// Close closes this memory data source
func (dataSource *MemoryDataSource) Close() error {
	if dataSource.closed {
		return errors.New("this datasource is already closed")
	}
	dataSource.closed = true
	close(dataSource.add)
	close(dataSource.remove)
	return nil
}

// Dial connects the data source
func (dataSource *MemoryDataSource) Dial() error {
	return nil
}

// CreateMemoryDataSource creates a memory data source for testing or logtail.
func CreateMemoryDataSource() *MemoryDataSource {
	return &MemoryDataSource{
		closed: false,
		add:    make(chan LogRoute, 1),
		remove: make(chan LogRoute, 1),
		routes: make([]LogRoute, 0),
	}
}
