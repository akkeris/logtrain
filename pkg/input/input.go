package input

import (
	syslog "github.com/papertrail/remote_syslog2/syslog"
)

type Input interface {
	Close() error
	Dial() error
	Errors() chan error
	Packets() chan syslog.Packet
	Pools() bool /* Whether the transport layer automatically pools or not. */
}

// TODO: input type "directory"...
// TODO: special input type persistent s3 storage?...
