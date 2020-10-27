package storage

type PostgresDataSource struct {
	add chan LogRoute
	remove chan LogRoute
	routes []LogRoute
}

func (kds *PostgresDataSource) AddRoute() chan LogRoute {
	return kds.add
}

func (kds *PostgresDataSource) RemoveRoute() chan LogRoute {
	return kds.remove
}

func (kds *PostgresDataSource) GetAllRoutes() ([]LogRoute, error) {
	return kds.routes, nil
}

func CreatePostgresDataSource() (*PostgresDataSource, error) {
	return &PostgresDataSource{
		add: make(chan LogRoute, 1),
		remove: make(chan LogRoute, 1),
		routes: make([]LogRoute, 0),
	}, nil
}