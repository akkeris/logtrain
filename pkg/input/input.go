package input

import (
	"errors"
	syslog "github.com/papertrail/remote_syslog2/syslog"
)

type Input interface {
	Close() error
	Dial() error
	Errors() chan error
	Packets() chan syslog.Packet
	Pools() bool /* Whether the transport layer automatically pools or not. */
}

func Create(endpoint string) (Input, error) {
	return nil, errors.New("Unrecognized endpoint " + endpoint)
}
