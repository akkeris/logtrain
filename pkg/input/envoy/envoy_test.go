package envoy

import (
	"context"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v2data "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v2"
	v2 "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"
	"github.com/golang/protobuf/ptypes"
	. "github.com/smartystreets/goconvey/convey"
	grpc "google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"io"
	"log"
	"testing"
	"time"
)

func TestEnvoyGrpcInput(t *testing.T) {
	var envoy *EnvoyAlsServer = nil
	var err error
	Convey("Ensure we can start the envoy server", t, func() {
		envoy, err = Create(":9001")
		So(envoy, ShouldNotBeNil)
		So(err, ShouldBeNil)
		So(envoy.Dial(), ShouldBeNil)
		So(envoy.Dial(), ShouldNotBeNil)
		So(envoy.Pools(), ShouldEqual, true)
	})
	Convey("Ensure we can receive logs from envoy", t, func() {
		grpcConn, err := grpc.Dial("localhost:9001", grpc.WithInsecure())
		if err != nil {
			log.Fatal(err)
		}
		So(grpcConn, ShouldNotBeNil)
		So(err, ShouldBeNil)
		accessLogServiceClient := v2.NewAccessLogServiceClient(grpcConn)
		client, err := accessLogServiceClient.StreamAccessLogs(context.TODO())
		So(client, ShouldNotBeNil)
		So(err, ShouldBeNil)

		// https://github.com/envoyproxy/go-control-plane/blob/bd7e4474d763c1ba0b91757b3ff069d72f211fed/envoy/service/accesslog/v2/als.pb.go#L362
		// https://github.com/envoyproxy/go-control-plane/blob/bd7e4474d763c1ba0b91757b3ff069d72f211fed/envoy/service/accesslog/v2/als.pb.go#L62
		// https://github.com/envoyproxy/envoy/blob/82e7109c73799197075654808f05c3aab33d2f2b/api/envoy/service/accesslog/v2/als.proto#L36
		message := &v2.StreamAccessLogsMessage{
			Identifier: &v2.StreamAccessLogsMessage_Identifier{
				Node:    &core.Node{},
				LogName: "test",
			},
			LogEntries: &v2.StreamAccessLogsMessage_HttpLogs{
				HttpLogs: &v2.StreamAccessLogsMessage_HTTPAccessLogEntries{
					LogEntry: []*v2data.HTTPAccessLogEntry{
						&v2data.HTTPAccessLogEntry{
							CommonProperties: &v2data.AccessLogCommon{
								TimeToLastUpstreamTxByte:   ptypes.DurationProto(time.Second),
								TimeToLastUpstreamRxByte:   ptypes.DurationProto(time.Second),
								TimeToLastDownstreamTxByte: ptypes.DurationProto(time.Second),
								UpstreamCluster:            "|||name.namespace",
								TlsProperties: &v2data.TLSProperties{
									TlsVersion:     v2data.TLSProperties_TLSv1_2,
									TlsSniHostname: "www.example.com",
								},
							},
							ProtocolVersion: v2data.HTTPAccessLogEntry_HTTP2,
							Request: &v2data.HTTPRequestProperties{
								Path:                "/fee",
								RequestMethod:       core.RequestMethod_POST,
								Authority:           "authority",
								ForwardedFor:        "1.1.1.1",
								RequestId:           "x-request-id",
								RequestBodyBytes:    100,
								RequestHeadersBytes: 200,
							},
							Response: &v2data.HTTPResponseProperties{
								ResponseCode: &wrapperspb.UInt32Value{
									Value: 200,
								},
								ResponseBodyBytes:    200,
								ResponseHeadersBytes: 100,
							},
						},
					},
				},
			},
		}
		if err := client.Send(message); err != nil {
			log.Fatal(err)
		}
		_, err = client.CloseAndRecv()
		So(err, ShouldEqual, io.EOF)
		select {
		case message := <-envoy.Packets():
			So(message.Severity, ShouldEqual, 0)
			So(message.Facility, ShouldEqual, 0)
			So(message.Hostname, ShouldEqual, "name.namespace")
			So(message.Tag, ShouldEqual, "envoy")
			So(message.Message, ShouldEqual, "bytes=600 request_size=300 response_size=300 method=POST request_id=x-request-id fwd=1.1.1.1 authority=authority origin=https://www.example.com protocol=http2 tls=TLSv1_2 status=200 connect=1000.00ms service=1000.00ms total=1000.00ms path=/fee")
		case <-envoy.Errors():
			log.Fatal("This shouldnt have been reached (error path).")
		default:
			log.Fatal("This shouldnt have been reached.")
		}
	})

	Convey("Ensure we can shutdown the envoy service", t, func() {
		So(envoy.Close(), ShouldBeNil)
	})
}
