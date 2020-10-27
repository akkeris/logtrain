package storage

type KubernetesDataSource struct {
	add chan LogRoute
	remove chan LogRoute
	routes []LogRoute
}

func (kds *KubernetesDataSource) AddRoute() chan LogRoute {
	return kds.add
}

func (kds *KubernetesDataSource) RemoveRoute() chan LogRoute {
	return kds.remove
}

func (kds *KubernetesDataSource) GetAllRoutes() ([]LogRoute, error) {
	return kds.routes, nil
}

func CreateKubernetesDataSource() (*KubernetesDataSource, error) {
	return &KubernetesDataSource{
		add: make(chan LogRoute, 1),
		remove: make(chan LogRoute, 1),
		routes: make([]LogRoute, 0),
	}, nil
}