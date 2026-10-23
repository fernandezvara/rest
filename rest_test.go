package rest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func freeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func waitListening(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
			c.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s is not listening", addr)
}

// startServer serves rr in the background, the returned channel receives Start's result
func startServer(t *testing.T, rr *REST) chan error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- rr.Start() }()
	return done
}

func TestNewBuildsHTTPServer(t *testing.T) {
	l, _ := newTestLogging(t)

	rr, err := New("127.0.0.1:1234", nil, nil, nil, false, l)
	if err != nil {
		t.Fatal(err)
	}
	if rr.httpServer == nil {
		t.Fatal("httpServer must exist right after New")
	}
	if rr.httpServer.Addr != "127.0.0.1:1234" || rr.httpServer.MaxHeaderBytes != 1<<20 || rr.httpServer.ErrorLog == nil {
		t.Errorf("unexpected server config: %+v", rr.httpServer)
	}
}

func TestStartServesAndShutdown(t *testing.T) {
	addr := freeAddr(t)
	l, _ := newTestLogging(t)
	rr, err := New(addr, nil, nil, nil, false, l)
	if err != nil {
		t.Fatal(err)
	}
	rr.SetupRouter(map[string]map[string]APIEndpoint{
		"GET": {"/status": {Handler: okHandler("alive")}},
	})

	done := startServer(t, rr)
	waitListening(t, addr)

	resp, err := http.Get("http://" + addr + "/status")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "alive" {
		t.Errorf("status = %d, body = %q", resp.StatusCode, body)
	}

	if err := rr.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			t.Errorf("Start returned %v, want ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}

// Shutdown used to dereference a nil httpServer when it ran before Start.
func TestShutdownBeforeStart(t *testing.T) {
	l, _ := newTestLogging(t)
	rr, err := New(freeAddr(t), nil, nil, nil, false, l)
	if err != nil {
		t.Fatal(err)
	}
	rr.SetupRouter(nil)

	if err := rr.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := rr.Start(); err != http.ErrServerClosed {
		t.Errorf("Start after Shutdown returned %v, want ErrServerClosed", err)
	}
}

// Start, Shutdown and SetCertificate run from different goroutines, this is meant to be run with -race.
func TestConcurrentStartShutdown(t *testing.T) {
	for i := 0; i < 20; i++ {
		l, _ := newTestLogging(t)
		rr, err := New(freeAddr(t), nil, nil, nil, false, l)
		if err != nil {
			t.Fatal(err)
		}
		rr.SetupRouter(nil)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); rr.Start() }()
		go func() { defer wg.Done(); rr.Shutdown() }()
		wg.Wait()
	}
}

func TestStartTCPPortInUse(t *testing.T) {
	l, _ := newTestLogging(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	rr, _ := New(occupied.Addr().String(), nil, nil, nil, false, l)
	rr.SetupRouter(nil)

	if err := rr.Start(); err == nil || err == http.ErrServerClosed {
		t.Errorf("Start = %v, want a listen error", err)
	}
}

type testPKI struct {
	caPEM []byte
	ca    *x509.Certificate
	caKey *ecdsa.PrivateKey
}

func newTestPKI(t *testing.T) *testPKI {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testPKI{
		caPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		ca:    ca,
		caKey: key,
	}
}

// issue returns a PEM server certificate/key for 127.0.0.1 with the given serial
func (p *testPKI) issue(t *testing.T, serial int64) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.ca, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// serverSerial connects over TLS verifying against the test CA and returns the served certificate serial
func (p *testPKI) serverSerial(t *testing.T, addr string) int64 {
	t.Helper()

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(p.caPEM)

	conn, err := tls.Dial("tcp", addr, &tls.Config{RootCAs: pool})
	if err != nil {
		t.Fatalf("tls dial: %v", err)
	}
	defer conn.Close()

	return conn.ConnectionState().PeerCertificates[0].SerialNumber.Int64()
}

func TestTLSServerAndCertificateRotation(t *testing.T) {
	pki := newTestPKI(t)
	cert1, key1 := pki.issue(t, 100)
	cert2, key2 := pki.issue(t, 200)

	addr := freeAddr(t)
	l, _ := newTestLogging(t)
	rr, err := New(addr, cert1, key1, pki.caPEM, false, l)
	if err != nil {
		t.Fatal(err)
	}
	rr.SetupRouter(map[string]map[string]APIEndpoint{
		"GET": {"/status": {Handler: okHandler("secure")}},
	})

	done := startServer(t, rr)
	defer func() {
		rr.Shutdown()
		<-done
	}()
	waitListening(t, addr)

	if got := pki.serverSerial(t, addr); got != 100 {
		t.Fatalf("serial = %d, want 100", got)
	}

	// HSTS must be sent over TLS
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pki.caPEM)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := client.Get("https://" + addr + "/status")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if got := resp.Header.Get("Strict-Transport-Security"); got == "" {
		t.Error("Strict-Transport-Security expected over TLS")
	}

	// rotate the certificate while the server is running
	if err := rr.SetCertificate(cert2, key2); err != nil {
		t.Fatalf("SetCertificate: %v", err)
	}
	if got := pki.serverSerial(t, addr); got != 200 {
		t.Errorf("serial after rotation = %d, want 200", got)
	}

	// an invalid pair is rejected and the previous certificate keeps being served
	if err := rr.SetCertificate(cert2, key1); err == nil {
		t.Error("mismatched certificate/key must be rejected")
	}
	if got := pki.serverSerial(t, addr); got != 200 {
		t.Errorf("serial after failed rotation = %d, want 200", got)
	}
}

func TestStartTLSWithInvalidCertificate(t *testing.T) {
	pki := newTestPKI(t)
	l, _ := newTestLogging(t)

	rr, _ := New(freeAddr(t), []byte("not a cert"), []byte("not a key"), pki.caPEM, false, l)
	rr.SetupRouter(nil)

	if err := rr.Start(); err == nil || err == http.ErrServerClosed {
		t.Errorf("Start = %v, want a certificate error", err)
	}
}

// SetCertificate used to panic when called before Start.
func TestSetCertificateBeforeStart(t *testing.T) {
	pki := newTestPKI(t)
	cert, key := pki.issue(t, 1)
	l, _ := newTestLogging(t)

	rr, _ := New(freeAddr(t), nil, nil, nil, false, l)
	if err := rr.SetCertificate(cert, key); err != nil {
		t.Fatalf("SetCertificate: %v", err)
	}
}

func TestTLSClientCertificateRequired(t *testing.T) {
	pki := newTestPKI(t)
	cert, key := pki.issue(t, 1)

	addr := freeAddr(t)
	l, _ := newTestLogging(t)
	rr, _ := New(addr, cert, key, pki.caPEM, true, l)
	rr.SetupRouter(nil)

	done := startServer(t, rr)
	defer func() {
		rr.Shutdown()
		<-done
	}()
	waitListening(t, addr)

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pki.caPEM)

	// TLS 1.3 reports the missing client certificate on the first read, not on the handshake
	conn, err := tls.Dial("tcp", addr, &tls.Config{RootCAs: pool})
	if err == nil {
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		fmt.Fprint(conn, "GET /status HTTP/1.0\r\n\r\n")
		_, err = conn.Read(make([]byte, 1))
	}
	if err == nil {
		t.Error("connection without client certificate must be rejected")
	}
}
