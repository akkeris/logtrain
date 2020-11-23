package storage

import (
	"errors"
	"github.com/akkeris/logtrain/internal/debug"
)

// TODO: Support regex in the hostname.
// TODO: Add datasource that's a configmap.
// TODO: Add a command line data source

// Datasource describes an interface for querying and listening for routes
type DataSource interface {
	AddRoute() chan LogRoute
	RemoveRoute() chan LogRoute
	GetAllRoutes() ([]LogRoute, error)
	EmitNewRoute(route LogRoute) error
	EmitRemoveRoute(route LogRoute) error
	Writable() bool
	Close() error
}

// LogRoute describes a structure for routes from Hostname -> Endpoint
type LogRoute struct {
	Endpoint      string
	Hostname      string
	Tag           string
	failedToWrite int
}

// FindDataSources finds what datasources may be availabl and returns instantiated objects
func FindDataSources(useKubernetes bool, kubeConfig string, usePostgres bool, databaseURL string) ([]DataSource, error) {
	ds := make([]DataSource, 0)

	if useKubernetes {
		k8sClient, err := GetKubernetesClient(kubeConfig)
		if err != nil {
			debug.Errorf("Could not get kubernetes client [%s]: %s\n", kubeConfig, err.Error())
			return nil, err
		}
		kds, err := CreateKubernetesDataSource(k8sClient, true)
		if err != nil {
			return nil, err
		}
		ds = append(ds, kds)
	}

	if usePostgres {
		if databaseURL == "" {
			return nil, errors.New("the database url was blank or empty")
		}
		pds, err := CreatePostgresDataSourceWithURL(databaseURL)
		if err != nil {
			return nil, err
		}
		ds = append(ds, pds)
	}

	return ds, nil
}
