package syslogtls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	syslog "github.com/papertrail/remote_syslog2/syslog"
	. "github.com/smartystreets/goconvey/convey"
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

func CreateDummyCerts(servername string) ([]byte, []byte) {
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
		DNSNames:              []string{servername},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("0.0.0.0")},
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
	return certPem.Bytes(), keyPem.Bytes()
}

func TestSyslogTlsInput(t *testing.T) {
	cert, key := CreateDummyCerts("localhost")
	server, err := Create("localhost", string(key), string(cert), "", "0.0.0.0:9002")
	if err != nil {
		log.Fatal(err)
	}

	Convey("Ensure nothing blows up on the stubs", t, func() {
		So(server.Dial(), ShouldBeNil)
		So(server.Dial(), ShouldNotBeNil)
		So(server.Pools(), ShouldEqual, true)
	})

	Convey("Ensure we can receive messages via the http syslog stream payload", t, func() {
		p := syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "Oh hello",
			Tag:      "web",
			Hostname: "name-namespace",
			Time:     time.Now(),
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		So(pool.AppendCertsFromPEM(cert), ShouldEqual, true)

		logger, err := syslog.Dial("test", "tls", "0.0.0.0:9002", pool, time.Second, time.Second, 1024)
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			select {
			case err := <-logger.Errors:
				if err != nil {
					log.Fatal(err)
				}
			}
		}()
		logger.Write(p)
		select {
		case message := <-server.Packets():
			So(p.Message+"\n", ShouldEqual, message.Message)
			So(p.Tag, ShouldEqual, message.Tag)
			So(p.Hostname, ShouldEqual, message.Hostname)
			So(p.Severity, ShouldEqual, message.Severity)
			So(p.Facility, ShouldEqual, message.Facility)
		case error := <-server.Errors():
			log.Fatal(error.Error())
		}
		logger.Close()
	})
	Convey("Ensure we can close the connection", t, func() {
		So(server.Close(), ShouldBeNil)
	})
}
