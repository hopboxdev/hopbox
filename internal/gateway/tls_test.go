package gateway_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mesadev/mesa/internal/gateway"
)

func TestSelfSignedCertHasWildcardSAN(t *testing.T) {
	cert, err := gateway.SelfSignedCert("gw.example.com")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.VerifyHostname("app-w1.gw.example.com"); err != nil {
		t.Fatalf("wildcard host should verify: %v", err)
	}
	if err := leaf.VerifyHostname("other.com"); err == nil {
		t.Fatal("a foreign host must not verify")
	}
}

func TestGatewayServesHTTPS(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "secure "+r.Host)
	}))
	defer backend.Close()
	backendAddr := backend.Listener.Addr().String()

	g := gateway.New(connectorFunc(func(_ context.Context, host string) (net.Conn, error) {
		if host == "app-w1.gw.example.com" {
			return net.Dial("tcp", backendAddr)
		}
		return nil, gateway.ErrNoRoute
	}))

	cert, err := gateway.SelfSignedCert("gw.example.com")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}})
	srv := &http.Server{Handler: g}
	go func() { _ = srv.Serve(tlsLn) }()
	defer srv.Close()

	// client trusts the self-signed leaf and presents the wildcard SNI
	pool := x509.NewCertPool()
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	pool.AddCert(leaf)
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "app-w1.gw.example.com"},
	}}

	req, _ := http.NewRequest("GET", "https://"+tlsLn.Addr().String()+"/", nil)
	req.Host = "app-w1.gw.example.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("https request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "secure app-w1.gw.example.com" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}
