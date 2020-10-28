# Logtrain

[![Codacy Badge](https://app.codacy.com/project/badge/Grade/28e234bd2afa4e0fac65da9944667aa8)](https://www.codacy.com/gh/akkeris/logtrain/dashboard?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=akkeris/logtrain&amp;utm_campaign=Badge_Grade)
[![Codacy Badge](https://app.codacy.com/project/badge/Coverage/28e234bd2afa4e0fac65da9944667aa8)](https://www.codacy.com/gh/akkeris/logtrain/dashboard?utm_source=github.com&utm_medium=referral&utm_content=akkeris/logtrain&utm_campaign=Badge_Coverage)

## Running

## Using Logtrain API

## Using Logtrain with Kubernetes Deployments

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