package storage

// TODO: Support regex in the hostname.
// TODO: Add datasource that's a configmap.
// TODO: Add a command line data source

// Datasource describes an interface for querying and listening for routes
type DataSource interface {
	AddRoute() chan LogRoute
	RemoveRoute() chan LogRoute
	GetAllRoutes() ([]LogRoute, error)
}

// LogRoute describes a structure for routes from Hostname -> Endpoint
type LogRoute struct {
	Endpoint      string
	Hostname      string
	Tag           string
	failedToWrite int
}
