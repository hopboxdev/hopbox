// Command hopbox-gw is the standalone Hopbox service gateway: a stateless HTTP(S)
// reverse proxy. It terminates inbound traffic and forwards each request into
// the target workspace over a tunnel to a central hopboxd, which resolves the
// request Host and bridges to the workspace's agent reverse-connection. hopbox-gw
// owns no state (no route table, no agent sessions), so it scales horizontally.
//
// TLS: routing is by Host header, so a single wildcard certificate (*.gw.host)
// serves unlimited per-workspace subdomains. Supply a real cert with
// --tls-cert/--tls-key (production), or --tls-self-signed for local testing.
package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/hopboxdev/hopbox/internal/gateway"
)

func main() {
	fs := flag.NewFlagSet("hopbox-gw", flag.ExitOnError)
	listen := fs.String("listen", ":8088", "HTTP(S) listen address (use :443 with TLS)")
	tunnel := fs.String("tunnel", "localhost:7701", "hopboxd gateway tunnel address")
	tlsCert := fs.String("tls-cert", "", "path to the wildcard TLS certificate (PEM)")
	tlsKey := fs.String("tls-key", "", "path to the TLS private key (PEM)")
	selfSigned := fs.Bool("tls-self-signed", false, "serve HTTPS with an in-memory self-signed wildcard cert (testing)")
	zone := fs.String("zone", "gw.example.com", "wildcard DNS zone for the self-signed cert SAN")
	redirect := fs.String("redirect-addr", "", "if set, serve an HTTP->HTTPS 301 redirect on this address (e.g. :80)")
	askAddr := fs.String("ask-addr", "", "if set, serve a Caddy on-demand-TLS ask endpoint here: 200 for hosts in --zone, else 403")
	_ = fs.Parse(os.Args[1:])

	gw := gateway.New(gateway.NewRemoteConnector(*tunnel))
	srv := &http.Server{Addr: *listen, Handler: gw}

	// Caddy on-demand TLS asks this endpoint before issuing a cert for a host,
	// bounding issuance to our wildcard zone (when TLS is fronted by Caddy and
	// hopbox-gw itself serves plain HTTP).
	if *askAddr != "" {
		z := strings.TrimPrefix(*zone, ".")
		go func() {
			log.Printf("hopbox-gw: on-demand-TLS ask on %s (zone %s)", *askAddr, z)
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				d := r.URL.Query().Get("domain")
				if d == z || strings.HasSuffix(d, "."+z) {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusForbidden)
			})
			if err := http.ListenAndServe(*askAddr, h); err != nil {
				log.Printf("hopbox-gw: ask server: %v", err)
			}
		}()
	}

	if *redirect != "" {
		go func() {
			log.Printf("hopbox-gw: HTTP->HTTPS redirect on %s", *redirect)
			if err := http.ListenAndServe(*redirect, http.HandlerFunc(redirectToHTTPS)); err != nil {
				log.Printf("hopbox-gw: redirect server: %v", err)
			}
		}()
	}

	switch {
	case *tlsCert != "" && *tlsKey != "":
		log.Printf("hopbox-gw: HTTPS on %s (cert %s), tunneling to hopboxd %s", *listen, *tlsCert, *tunnel)
		log.Fatal(srv.ListenAndServeTLS(*tlsCert, *tlsKey))
	case *selfSigned:
		cert, err := gateway.SelfSignedCert(*zone)
		if err != nil {
			log.Fatalf("hopbox-gw: self-signed cert: %v", err)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		log.Printf("hopbox-gw: HTTPS on %s (self-signed *.%s), tunneling to hopboxd %s", *listen, *zone, *tunnel)
		log.Fatal(srv.ListenAndServeTLS("", ""))
	default:
		log.Printf("hopbox-gw: HTTP on %s, tunneling to hopboxd %s", *listen, *tunnel)
		log.Fatal(srv.ListenAndServe())
	}
}

// redirectToHTTPS 301s any request to the https:// form of the same host+path.
func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	target := "https://" + r.Host + r.URL.RequestURI()
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}
