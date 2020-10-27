package storage

type DataSource interface {
	AddRoute() chan LogRoute
	RemoveRoute() chan LogRoute
	GetAllRoutes() ([]LogRoute, error)
}

type LogRoute struct {
	Endpoint string
	Hostname string
	Tag string
	failedToWrite int
}