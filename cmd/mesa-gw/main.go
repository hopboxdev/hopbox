// Command mesa-gw is the standalone Mesa service gateway: a stateless HTTP
// reverse proxy. It terminates inbound HTTP and forwards each request into the
// target workspace over a tunnel to a central mesad, which resolves the request
// Host and bridges to the workspace's agent reverse-connection. mesa-gw owns no
// state — no route table, no agent sessions — so it scales horizontally behind
// a load balancer. (TLS termination + wildcard-DNS routing are deployment
// concerns layered in front; this serves plain HTTP, routing by Host header.)
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/mesadev/mesa/internal/gateway"
)

func main() {
	fs := flag.NewFlagSet("mesa-gw", flag.ExitOnError)
	listen := fs.String("listen", ":8088", "HTTP listen address")
	tunnel := fs.String("tunnel", "localhost:7701", "mesad gateway tunnel address")
	_ = fs.Parse(os.Args[1:])

	gw := gateway.New(gateway.NewRemoteConnector(*tunnel))
	log.Printf("mesa-gw: listening on %s, tunneling to mesad %s", *listen, *tunnel)
	if err := http.ListenAndServe(*listen, gw); err != nil {
		log.Fatalf("mesa-gw: %v", err)
	}
}
