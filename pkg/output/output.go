package output

import (
	"errors"
	elasticsearch "github.com/akkeris/logtrain/pkg/output/elasticsearch"
	http "github.com/akkeris/logtrain/pkg/output/http"
	memory "github.com/akkeris/logtrain/pkg/output/memory"
	persistent "github.com/akkeris/logtrain/pkg/output/persistent"
	sysloghttp "github.com/akkeris/logtrain/pkg/output/sysloghttp"
	syslogtcp "github.com/akkeris/logtrain/pkg/output/syslogtcp"
	syslogtls "github.com/akkeris/logtrain/pkg/output/syslogtls"
	syslogudp "github.com/akkeris/logtrain/pkg/output/syslogudp"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
)

type Output interface {
	Close() error
	Dial() error
	Packets() chan syslog.Packet
	Pools() bool /* Whether the transport layer automatically pools or not. */
}

func TestEndpoint(endpoint string) error {
	if elasticsearch.Test(endpoint) == false &&
		syslogtls.Test(endpoint) == false &&
		syslogtcp.Test(endpoint) == false &&
		syslogudp.Test(endpoint) == false &&
		sysloghttp.Test(endpoint) == false &&
		memory.Test(endpoint) == false &&
		http.Test(endpoint) == false && 
		persistent.Test(endpoint) == false {
		return errors.New("unrecognized schema type")
	}
	return nil
}

func Create(endpoint string, errorsCh chan<- error) (Output, error) {
	if elasticsearch.Test(endpoint) == true {
		return elasticsearch.Create(endpoint, errorsCh)
	} else if http.Test(endpoint) == true {
		return http.Create(endpoint, errorsCh)
	} else if sysloghttp.Test(endpoint) == true {
		return sysloghttp.Create(endpoint, errorsCh)
	} else if syslogtcp.Test(endpoint) == true {
		return syslogtcp.Create(endpoint, errorsCh)
	} else if syslogtls.Test(endpoint) == true {
		return syslogtls.Create(endpoint, errorsCh)
	} else if syslogudp.Test(endpoint) == true {
		return syslogudp.Create(endpoint, errorsCh)
	} else if memory.Test(endpoint) == true {
		return memory.Create(endpoint, errorsCh)
	} else if persistent.Test(endpoint) == true {
		return persistent.Create(endpoint, errorsCh)
	}
	return nil, errors.New("Unrecognized endpoint " + endpoint)
}
