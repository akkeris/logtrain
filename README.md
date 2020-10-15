
## Running

## Using Logtrain API

## Using Logtrain with Kubernetes Deployments

## Drain Types

* `elasticsearch://host/bulk/`
* `http://host/path`
* `https://host/path`
* `syslog+tls://host:port?[ca=]`
* `syslog+http://host:port`
* `syslog+https://host:port`
* `syslog+tcp://host:port`
* `syslog+udp://` (aliases: `syslog://`)

## Configuration

### Akkeris

* `AKKERIS=true` - for Akkeris formatting of output.

### Envoy

* `ENVOY=true`
* `ENVOY_PORT=9001`

## Developing

### Building

```
go build .
```

### Testing

```
go test -v .../.
```

### Code Coverage

```
go test -coverprofile cover.out -v ./... && go tool cover -html=cover.out && rm cover.out
```