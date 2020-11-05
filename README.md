# Logtrain

[![Codacy Badge](https://app.codacy.com/project/badge/Grade/28e234bd2afa4e0fac65da9944667aa8)](https://www.codacy.com/gh/akkeris/logtrain/dashboard?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=akkeris/logtrain&amp;utm_campaign=Badge_Grade)
[![Codacy Badge](https://app.codacy.com/project/badge/Coverage/28e234bd2afa4e0fac65da9944667aa8)](https://www.codacy.com/gh/akkeris/logtrain/dashboard?utm_source=github.com&utm_medium=referral&utm_content=akkeris/logtrain&utm_campaign=Badge_Coverage)

## Overview

Logtrain is a system for dynamically forwarding and transforming logs, similar to fluentd but a bit more specialized to solve two issues...

1. Have very low overhead (e.g., less than 64Mi)
2. Dynamically route logs based on various data sources.

## Drain Types

* `es+https://user:password@host?[auth=apikey|bearer|basic]` (aliases, `elasticsearch://` and `es://`). The bearer token is taken from the password portion of the url. 
* `es+http://user:password@host?[auth=apikey|bearer|basic]`
* `http://host/path`
* `https://host/path`
* `syslog+tls://host:port?[ca=]`
* `syslog+http://host:port`
* `syslog+https://host:port`
* `syslog+tcp://host:port`
* `syslog+udp://` (aliases, `syslog://`)

## Using Logtrain API

***TODO***

## Using Logtrain with Kubernetes

### Running in Kubernetes

Below is the recommended way of deploying Log Train. Note that this must run as a privileged container,
so that it can read files from `/var/log/containers`. The daemonset also contains an initContainer that
will change the sysctl `fs.inotify.max_user_instances` to `2048` on your nodes (usually from 128).

```
kubectl apply -f ./deployments/kubernetes/logtrain-serviceaccount.yaml
kubectl apply -f ./deployments/kubernetes/logtrain-service.yaml
kubectl apply -f ./deployments/kubernetes/logtrain-daemonset.yaml
```

### Adding drains in Kubernetes

Once deployed you can use the following annotations on deployments, daemonsets, statefulsets or replicasets to forward logs.

```
logtrain.akkeris.io/drains
```

This annoation is a comma delimited list of drains (See Drain Types above).

```
logtrain.akkeris.io/hostname
```

Explicitly set the hostname used when reading in logs from kubernetes, if not set this will default to the `name.namespace`.

```
logtrain.akkeris.io/tag
```

Explicitly set the tag when reading in logs from kuberntes, if not set this will default to the pod name.

## Using Logtrain with Servers

***TODO***

## Advanced Configuration

### General

* `HTTP_PORT` - The port to use for the http server, shared by any http (payload) and http (syslog) inputs.

### Postgres (datasource)

Whether to watch a postgres database for information on where to foward logs to.

* `POSTGRES` - set to `true`
* `DATABASE_URL` - The database url to use to listen for drain changes.

### Kubernetes (datasource)

Whether to watch kubernetes deployments, statefulsets and daemonsets for annotations indicating
where logs should be forwarded to.

* `KUBERNETES_DATASOURCE` - set to `true`

### Kubernetes

Whether to watch the `KUBERNETES_LOG_PATH` directory for pod logs and forward them.

* `KUBERNETES` - set to `true`
* `KUBERNETES_LOG_PATH` - optional, the path on each node to look for logs. Defaults to `/var/log/containers`

### Envoy/Istio

Whether to open a gRPC access log stream end point for istio/envoy to stream http log traffic to.

* `ENVOY` - set to `true`
* `ENVOY_PORT` - The port number to listen for gRPC access log streams (default is `9001`)

### Http (events)

* `HTTP_EVENTS` - set to `true`
* `HTTP_EVENTS_PATH` - optional, The path on the http server to receive http event payloads, defaults to `/events`

Note, the port is inherited from `HTTP_PORT`.  The endpoint only allows one per event over the body and must
be the format defined by [pkg/output/packet/packet.go](packet.go).

### Syslog (HTTP)

* `HTTP_SYSLOG` - set to `true`
* `HTTP_SYSLOG_PATH` - optional, The path on the http server to receive syslog streams as http, defaults to `/syslog`

Note, the port is inherited from `HTTP_PORT`.

###  Syslog (TCP)

* `SYSLOG_TCP` - set to `true`
* `SYSLOG_TCP_PORT` - optional, defaults tcp `9002`

### Syslog (UDP)

* `SYSLOG_UDP` - set to `true`
* `SYSLOG_UDP_PORT` - optional, defaults to `9003`

### Syslog (TLS)

* `SYSLOG_TLS` - set to `true`
* `SYSLOG_TLS_CERT_PEM` - The PEM encoded certificate 
* `SYSLOG_TLS_CA_PEM` - The PEM encoded certificate authority (optional)
* `SYSLOG_TLS_KEY_PEM` - The PEM encoded certificate key
* `SYSLOG_TLS_SERVER_NAME` - The servername the TLS server should use for SNI
* `SYSLOG_TLS_PORT` - optional, defaults to `9004`


### Akkeris Formatting (optional)

* `AKKERIS=true` - for Akkeris formatting of output

## Performance

The logtrain has been tested to be below 64MB (avg 59MB) and < 100m (5%) 
CPU for 52 pods on a node with 1500+ deployments being watched. For more pods per node
or more deployments than the benchmark expect (and reset any limits/requests) for memory.

While targeting a 64MB top limit, logtrain should have a limit of 128MB.

## Troubleshooting

### Too many open files error

If you receive an error on startup with `too many files open` error message you'll need to increase
the `fs.inotify.max_user_instances` and `user.max_inotify_instances`. These are generally set to
`128` by default, depending how many pods are running this may be insufficient.

```
sysctl -w fs.inotify.max_user_instances=2048
sysctl -w user.max_inotify_instances=2048
```

## Developing

### Building Logtrain

```
go build -o logtrain cmd/logtrain/main.go
```

### Testing

```
go test -v .../.
```

(Note if you're using GoConvey its best to set the parallel packges to 1 via `-packages 1`)

### Code Coverage

```
go test -coverprofile cover.out -v ./... && go tool cover -html=cover.out && rm cover.out
```