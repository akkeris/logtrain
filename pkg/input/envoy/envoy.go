package envoy

import (
	"errors"
	"fmt"
	v2data "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v2"
	v2 "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes"
	syslog "github.com/papertrail/remote_syslog2/syslog"
	"google.golang.org/grpc"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type EnvoyAlsServer struct {
	address   string
	marshaler jsonpb.Marshaler
	server    *grpc.Server
	errors    chan error
	packets   chan syslog.Packet
}

var _ v2.AccessLogServiceServer = &EnvoyAlsServer{}
var hostTemplate = "{name}.{namespace}"
var tag = "envoy"

func stringMilliseconds(d time.Duration) string {
	return fmt.Sprintf("%.2fms", d.Seconds()*1000)
}

// For more information on the HTTPAccessLogEntry structure see,
// https://github.com/envoyproxy/go-control-plane/blob/master/envoy/data/accesslog/v2/accesslog.pb.go
func layout(envoyMsg *v2data.HTTPAccessLogEntry) string {
	var code uint32 = 0
	if envoyMsg.CommonProperties.ResponseFlags != nil && envoyMsg.CommonProperties.ResponseFlags.DownstreamConnectionTermination {
		code = 499
	}

	if envoyMsg.Response.ResponseCode != nil {
		code = envoyMsg.Response.ResponseCode.GetValue()
	}

	var tls string = ""
	if envoyMsg.CommonProperties.GetTlsProperties() != nil {
		tls = "tls=" + envoyMsg.CommonProperties.GetTlsProperties().TlsVersion.String() + " "
	}

	var origin string = ""
	if envoyMsg.CommonProperties.GetTlsProperties() != nil {
		origin = "origin=https://" + envoyMsg.CommonProperties.GetTlsProperties().TlsSniHostname + envoyMsg.Request.OriginalPath + " "
	}

	// TODO: Add https://github.com/envoyproxy/go-control-plane/blob/master/envoy/data/accesslog/v2/accesslog.pb.go#L652

	ttlutxbyte, err := ptypes.Duration(envoyMsg.CommonProperties.TimeToLastUpstreamTxByte)
	if err != nil {
		ttlutxbyte = time.Millisecond
	}
	ttlurxbyte, err := ptypes.Duration(envoyMsg.CommonProperties.TimeToLastUpstreamRxByte)
	if err != nil {
		ttlurxbyte = time.Millisecond
	}
	ttldtxbyte, err := ptypes.Duration(envoyMsg.CommonProperties.TimeToLastDownstreamTxByte)
	if err != nil {
		ttldtxbyte = time.Millisecond
	}
	return "bytes=" + strconv.Itoa(int(envoyMsg.Request.RequestHeadersBytes+envoyMsg.Request.RequestBodyBytes+envoyMsg.Response.ResponseHeadersBytes+envoyMsg.Response.ResponseBodyBytes)) + " " +
		"request_size=" + strconv.Itoa(int(envoyMsg.Request.RequestHeadersBytes+envoyMsg.Request.RequestBodyBytes)) + " " +
		"response_size=" + strconv.Itoa(int(envoyMsg.Response.ResponseHeadersBytes+envoyMsg.Response.ResponseBodyBytes)) + " " +
		"method=" + envoyMsg.Request.RequestMethod.String() + " " +
		"request_id=" + envoyMsg.Request.RequestId + " " +
		"fwd=" + envoyMsg.Request.ForwardedFor + " " +
		"authority=" + envoyMsg.Request.Authority + " " +
		origin +
		"protocol=" + strings.ToLower(envoyMsg.ProtocolVersion.String()) + " " +
		tls +
		"status=" + strconv.Itoa(int(code)) + " " +
		"connect=" + stringMilliseconds(ttlutxbyte) + " " +
		"service=" + stringMilliseconds(ttlurxbyte) + " " +
		"total=" + stringMilliseconds(ttldtxbyte)
}

func (s *EnvoyAlsServer) Close() error {
	s.server.Stop()
	close(s.packets)
	close(s.errors)
	return nil
}

func (s *EnvoyAlsServer) StreamAccessLogs(stream v2.AccessLogService_StreamAccessLogsServer) error {
	s.marshaler.OrigName = true
	log.Println("Started envoy access log stream")
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			log.Printf("Failed to recieve istio access logs: %s\n", err.Error())
			return err
		}
		switch entries := in.LogEntries.(type) {
		case *v2.StreamAccessLogsMessage_HttpLogs:
			for _, entry := range entries.HttpLogs.LogEntry {
				c := strings.Split(entry.CommonProperties.UpstreamCluster, "|")
				if len(c) > 3 {
					d := strings.Split(c[3], ".")
					name := d[0]
					namespace := d[1]
					hostname := strings.Replace(strings.Replace(hostTemplate, "{name}", name, 1), "{namespace}", namespace, 1)
					t, err := ptypes.Timestamp(entry.CommonProperties.StartTime)
					if err != nil {
						t = time.Now()
					}
					s.packets <- syslog.Packet{
						Severity: 0,
						Facility: 0,
						Hostname: hostname,
						Time:     t,
						Tag:      tag,
						Message:  layout(entry),
					}
				}
			}
		}
	}
}

func (s *EnvoyAlsServer) Pools() bool {
	return true
}

func (s *EnvoyAlsServer) Packets() chan syslog.Packet {
	return s.packets
}

func (s *EnvoyAlsServer) Errors() chan error {
	return s.errors
}

func (s *EnvoyAlsServer) Dial() error {
	if s.server != nil {
		return errors.New("Dial may only be called once.")
	}
	s.server = grpc.NewServer()
	v2.RegisterAccessLogServiceServer(s.server, s)
	l, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	go s.server.Serve(l)
	return nil
}

func Create(address string) (*EnvoyAlsServer, error) {
	if address == "" {
		address = ":9001"
	}
	if os.Getenv("AKKERIS") == "true" {
		hostTemplate = "{name}-{namespace}"
		tag = "akkeris/router"
	}
	return &EnvoyAlsServer{
		address: address,
		server:  nil,
		packets: make(chan syslog.Packet, 100),
		errors:  make(chan error, 1),
	}, nil
}
