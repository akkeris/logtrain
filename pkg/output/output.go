package output

import (
	"errors"
	elasticsearch "github.com/akkeris/logtrain/pkg/output/elasticsearch"
	http "github.com/akkeris/logtrain/pkg/output/http"
	sysloghttp "github.com/akkeris/logtrain/pkg/output/sysloghttp"
	syslogtcp "github.com/akkeris/logtrain/pkg/output/syslogtcp"
	syslogtls "github.com/akkeris/logtrain/pkg/output/syslogtls"
	syslogudp "github.com/akkeris/logtrain/pkg/output/syslogudp"
	syslog "github.com/papertrail/remote_syslog2/syslog"
)

type Output interface {
	Close() error
	Dial() error
	Errors() chan error
	Packets() chan syslog.Packet
	Pools() bool /* Whether the transport layer automatically pools or not. */
}

func TestEndpoint(endpoint string) error {
	if elasticsearch.Test(endpoint) == false &&
		syslogtls.Test(endpoint) == false &&
		syslogtcp.Test(endpoint) == false &&
		syslogudp.Test(endpoint) == false &&
		sysloghttp.Test(endpoint) == false &&
		http.Test(endpoint) == false {
		return errors.New("Unrecognized schema type")
	}
	return nil
}

func Create(endpoint string) (Output, error) {
	if elasticsearch.Test(endpoint) == true {
		return elasticsearch.Create(endpoint)
	} else if http.Test(endpoint) == true {
		return http.Create(endpoint)
	} else if sysloghttp.Test(endpoint) == true {
		return sysloghttp.Create(endpoint)
	} else if syslogtcp.Test(endpoint) == true {
		return syslogtcp.Create(endpoint)
	} else if syslogtls.Test(endpoint) == true {
		return syslogtls.Create(endpoint)
	} else if syslogudp.Test(endpoint) == true {
		return syslogudp.Create(endpoint)
	}
	return nil, errors.New("Unrecognized endpoint " + endpoint)
}
