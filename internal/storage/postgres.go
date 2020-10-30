package storage

type PostgresDataSource struct {
	add    chan LogRoute
	remove chan LogRoute
	routes []LogRoute
}

func (pds *PostgresDataSource) AddRoute() chan LogRoute {
	return pds.add
}

func (pds *PostgresDataSource) RemoveRoute() chan LogRoute {
	return pds.remove
}

func (pds *PostgresDataSource) GetAllRoutes() ([]LogRoute, error) {
	return pds.routes, nil
}

func CreatePostgresDataSource() (*PostgresDataSource, error) {
	return &PostgresDataSource{
		add:    make(chan LogRoute, 1),
		remove: make(chan LogRoute, 1),
		routes: make([]LogRoute, 0),
	}, nil
}
