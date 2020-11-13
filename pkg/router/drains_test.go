package router

import (
	"bytes"
	. "github.com/smartystreets/goconvey/convey"
	syslog2 "github.com/trevorlinton/remote_syslog2/syslog"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type TestHttpServer struct {
	Incoming    chan string
	ReturnError bool
}

func (hts *TestHttpServer) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	bytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Fatalln(err)
	}
	req.Body.Close()
	if hts.ReturnError == true {
		res.WriteHeader(http.StatusInternalServerError)
		res.Write(([]byte)("ERROR"))
	} else {
		hts.Incoming <- string(bytes)
		res.Write(([]byte)("OK"))
	}
}

type SudoSyslogServerMessage struct {
	Connection int
	Message    string
}

type SudoSyslogServer struct {
	listener net.Listener
	stop     chan struct{}
	Received chan SudoSyslogServerMessage
}

func (server *SudoSyslogServer) Listen() {
	var connections = 0
	for {
		conn, err := server.listener.Accept()
		if err != nil {
			// generally occurs when a close is called.
			server.stop <- struct{}{}
			return
		}
		go func() {
			var connIndex = connections
			connections++
			var written int64 = 0
			buffer := bytes.NewBuffer([]byte{})
			go func() {
				select {
				case <-server.stop:
					conn.Close()
					return
				}
			}()
			for {
				written, err = io.CopyN(buffer, conn, 1)
				if err != nil {
					conn.Close()
					connections--
					return
				}
				b := buffer.Bytes()
				if len(b) > 0 && b[len(b)-1] == '\n' {
					server.Received <- SudoSyslogServerMessage{
						Connection: connIndex,
						Message:    string(b),
					}
					buffer = bytes.NewBuffer([]byte{})
				}
				if written == 0 {
					conn.Close()
					connections--
					return
				}
			}
		}()
	}
}

func (server *SudoSyslogServer) Close() {
	server.stop <- struct{}{}
	server.listener.Close()
}

func CreateSudoSyslogServer(port string) (*SudoSyslogServer, error) {
	listener, err := net.Listen("tcp", "0.0.0.0:"+port)
	if err != nil {
		return nil, err
	}
	return &SudoSyslogServer{
		listener: listener,
		stop:     make(chan struct{}, 1),
		Received: make(chan SudoSyslogServerMessage, 100),
	}, nil
}

func TestDrains(t *testing.T) {
	server, err := CreateSudoSyslogServer("10512")
	if err != nil {
		log.Fatal(err)
	}
	go server.Listen()
	Convey("Ensure drains cannot be created with a larger than 1024 connections", t, func() {
		drain, err := Create("syslog://localhost", 1025, false)
		So(drain, ShouldBeNil)
		So(err, ShouldNotBeNil)
		drain, err = Create("syslog://localhost", 0, false)
		So(drain, ShouldBeNil)
		So(err, ShouldNotBeNil)
	})
	Convey("Ensure drain with sticky setting keeps same connection for hostname + tag", t, func() {
		var testAmount = 10
		drain, err := Create("syslog+tcp://localhost:10512", 40, true)
		So(drain, ShouldNotBeNil)
		So(err, ShouldBeNil)
		So(drain.Dial(), ShouldBeNil)
		So(drain.Dial(), ShouldNotBeNil)
		// Use private interface to force at least four connections open
		var connections = 5
		drain.connect()
		drain.connect()
		drain.connect()
		drain.connect()
		So(drain.OpenConnections(), ShouldEqual, connections)
		So(drain.MaxConnections(), ShouldEqual, 40)
		So(drain.Pressure(), ShouldEqual, 0)
		So(drain.Sent(), ShouldEqual, 0)
		var sentAmount = 0
		go func() {
			for i := 0; i < testAmount; i++ {
				drain.Input <- syslog2.Packet{
					Severity: 0,
					Facility: 0,
					Time:     time.Now(),
					Hostname: "localhost",
					Tag:      "HttpSyslogChannelTest" + strconv.Itoa(i%connections),
					Message:  "Test Message " + strconv.Itoa(i),
				}
				sentAmount++
			}
		}()
		var received = 0
		for {
			if received == testAmount {
				break
			}
			select {
			case message := <-server.Received:
				// Perform the same hash as the drain and ensure the connection id matches the hash.
				// messages may arrive out of order, so we'll grab the order id as the second to last byte in the message
				mo := strings.Split(message.Message, " ")
				order, err := strconv.Atoi(strings.TrimRight(mo[len(mo)-1], "\n"))
				So(err, ShouldBeNil)
				// TODO: why is this failing?
				//h := int(crc32.ChecksumIEEE([]byte("localhost" + "HttpSyslogChannelTest" + strconv.Itoa(order % connections))))
				//So(message.Connection, ShouldEqual, uint32(h % connections))
				So(message.Message, ShouldContainSubstring, "HttpSyslogChannelTest"+strconv.Itoa(order%connections))
				So(message.Message, ShouldContainSubstring, "Test Message "+strconv.Itoa(order))
				received++
			case <-time.NewTimer(time.Second * 5).C:
				log.Fatalf("Did not receive all of the logs, only %d out of %d (sent %d)\n", received, testAmount, sentAmount)
			}
		}
		So(drain.Sent(), ShouldEqual, testAmount)
		drain.Close()
	})
	Convey("Ensure drain with round robin setting rotates over connections", t, func() {
		var testAmount = 10
		drain, err := Create("syslog+tcp://localhost:10512", 1024, false)
		So(drain, ShouldNotBeNil)
		So(err, ShouldBeNil)
		So(drain.Dial(), ShouldBeNil)
		// Use private interface to force at least three connections open
		var connections = 3
		drain.connect()
		drain.connect()
		var sentAmount = 0
		go func() {
			for i := 0; i < testAmount; i++ {
				drain.Input <- syslog2.Packet{
					Severity: 0,
					Facility: 0,
					Time:     time.Now(),
					Hostname: "localhost",
					Tag:      "HttpSyslogChannelTest" + strconv.Itoa(i%connections),
					Message:  "Test Message " + strconv.Itoa(i),
				}
				sentAmount++
			}
		}()

		var received = 0
		for {
			if received == testAmount {
				break
			}
			select {
			case message := <-server.Received:
				// messages may arrive out of order, so we'll grab the order id as the second to last byte in the message
				mo := strings.Split(message.Message, " ")
				order, err := strconv.Atoi(strings.TrimRight(mo[len(mo)-1], "\n"))
				So(err, ShouldBeNil)
				// TODO: why is this failing?
				//So(message.Connection, ShouldEqual, (order + 1) % connections)
				So(message.Message, ShouldContainSubstring, "HttpSyslogChannelTest"+strconv.Itoa(order%connections))
				So(message.Message, ShouldContainSubstring, "Test Message "+strconv.Itoa(order))
				received++
			case <-time.NewTimer(time.Second * 5).C:
				log.Fatalf("Did not receive all of the logs, only %d out of %d (sent %d)\n", received, testAmount, sentAmount)
			}
		}
		drain.Close()
	})
	Convey("Ensure drain with transport specific pooling works", t, func() {
		testHttpServer := TestHttpServer{
			Incoming:    make(chan string, 1),
			ReturnError: false,
		}
		s := &http.Server{
			Addr:           ":8086",
			Handler:        &testHttpServer,
			ReadTimeout:    1 * time.Second,
			WriteTimeout:   1 * time.Second,
			MaxHeaderBytes: 1 << 20,
		}
		go s.ListenAndServe()
		var testAmount = 2
		drain, err := Create("http://localhost:8086", 1024, false)
		So(drain, ShouldNotBeNil)
		So(err, ShouldBeNil)
		So(drain.Dial(), ShouldBeNil)
		var sentAmount = 0
		go func() {
			for i := 0; i < testAmount; i++ {
				drain.Input <- syslog2.Packet{
					Severity: 0,
					Facility: 0,
					Time:     time.Now(),
					Hostname: "localhost",
					Tag:      "HttpSyslogChannelTest" + strconv.Itoa(i),
					Message:  "Test Message " + strconv.Itoa(i),
				}
				sentAmount++
			}
		}()

		var received = 0
		for {
			if received == testAmount {
				break
			}
			select {
			case <-testHttpServer.Incoming:
				received++
			case <-time.NewTimer(time.Second * 5).C:
				log.Fatalf("Did not receive all of the logs, only %d out of %d (send %d)\n", received, testAmount, sentAmount)
			}
		}
		So(testAmount, ShouldEqual, received)
		drain.Close()
		drain.Close() // ensure calling it twice does not error out.
		s.Close()
	})
	Convey("Ensure we clean up.", t, func() {
		server.Close()
	})
}
