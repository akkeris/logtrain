package syslogtls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	. "github.com/smartystreets/goconvey/convey"
	syslog2 "github.com/trevorlinton/remote_syslog2/syslog"
	"io"
	"log"
	"math/big"
	"net"
	"testing"
	"time"
)

func publicKey(priv interface{}) interface{} {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return &k.PublicKey
	case *ecdsa.PrivateKey:
		return &k.PublicKey
	case ed25519.PrivateKey:
		return k.Public().(ed25519.PublicKey)
	default:
		return nil
	}
}

func CreateDummyTLSConfig() (*tls.Config, []byte) {
	var priv interface{}
	var err error
	priv, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		log.Fatal(err)
	}
	keyUsage := x509.KeyUsageDigitalSignature
	if _, isRSA := priv.(*rsa.PrivateKey); isRSA {
		keyUsage |= x509.KeyUsageKeyEncipherment
	}
	notBefore := time.Now().Add(time.Hour * -1)
	notAfter := notBefore.Add(time.Hour * 10)
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
		},
		DNSNames:              []string{"localhost"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              keyUsage,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, publicKey(priv), priv)

	certPem := bytes.NewBuffer([]byte{})
	keyPem := bytes.NewBuffer([]byte{})

	if err := pem.Encode(certPem, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		log.Fatal(err)
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err := pem.Encode(keyPem, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		log.Fatal(err)
	}

	cert, err := tls.X509KeyPair(certPem.Bytes(), keyPem.Bytes())
	if err != nil {
		log.Fatal(err)
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		log.Fatal(err)
	}
	roots.AppendCertsFromPEM(certPem.Bytes())
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, ServerName: "localhost", ClientAuth: tls.NoClientCert, RootCAs: roots}
	return cfg, certPem.Bytes()
}

func CreateTlsSudoSyslogServer(port string) (chan string, chan struct{}, net.Listener, []byte) {
	channel := make(chan string, 1)
	stop := make(chan struct{}, 1)
	listener, err := net.Listen("tcp", "0.0.0.0:"+port)
	cfg, cert := CreateDummyTLSConfig()
	if err != nil {
		log.Fatal(err.Error())
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Accept errored\n")
				log.Fatal(err)
			}
			server := tls.Server(conn, cfg)
			err = server.Handshake()
			if err != nil {
				log.Printf("handshake failed:\n")
				log.Fatal(err)
			}
			var written int64 = 0
			buffer := bytes.NewBuffer([]byte{})
			for {
				go func() {
					select {
					case <-stop:
						conn.Close()
						server.Close()
						return
					}
				}()
				written, err = io.CopyN(buffer, server, 1)
				if err != nil {
					server.Close()
					return
				}
				b := buffer.Bytes()
				if len(b) > 0 && b[len(b)-1] == '\n' {
					channel <- string(b)
					buffer = bytes.NewBuffer([]byte{})
				}
				if written == 0 {
					break
				}
			}
			server.Close()
		}
	}()
	return channel, stop, listener, cert
}

func TestSyslogTlsOutput(t *testing.T) {
	errorCh := make(chan error, 1)
	channel, stop, server, cert := CreateTlsSudoSyslogServer("8514")
	syslog, err := Create("syslog+tls://localhost:8514/?ca="+base64.StdEncoding.EncodeToString(cert), errorCh)
	if err != nil {
		log.Fatal(err.Error())
	}
	Convey("Ensure syslog is created", t, func() {
		So(err, ShouldBeNil)
	})
	Convey("Ensure that an syslog+tls explicitly does not pool connections.", t, func() {
		So(syslog.Pools(), ShouldEqual, false)
	})
	Convey("Ensure we can start the tls syslog end point on port 8514.", t, func() {
		So(syslog.Dial(), ShouldBeNil)
	})
	Convey("Ensure we can send syslog packets", t, func() {
		now := time.Now()
		p := syslog2.Packet{
			Severity: 0,
			Facility: 0,
			Time:     now,
			Hostname: "localhost",
			Tag:      "HttpSyslogChannelTest",
			Message:  "Test Message",
		}
		syslog.Packets() <- p
		select {
		case message := <-channel:
			So(message, ShouldEqual, p.Generate(MaxLogSize)+"\n")
		case error := <-errorCh:
			log.Fatal(error.Error())
		}
	})
	// TODO: How to test errors?
	Convey("Ensure we can close a syslog end point...", t, func() {
		stop <- struct{}{}
		server.Close()
		So(syslog.Close(), ShouldBeNil)
	})
	Convey("Ensure nonsensical base64 cas wont work", t, func() {
		_, err := Create("syslog+tls://localhost:8514/?ca=blah", errorCh)
		So(err, ShouldNotBeNil)
		_, err = Create("syslog+tls://localhost:8514/?ca="+base64.StdEncoding.EncodeToString([]byte("this is not a valid pem file.")), errorCh)
		So(err, ShouldNotBeNil)
	})
}
