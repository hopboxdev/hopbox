// Command hopbox-provider serves a single Hopbox provider over gRPC (the remote
// transport). hopboxd (or another client) dials it via --compute-remote/--storage-remote.
package main

import (
	"flag"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"

	pb "github.com/hopboxdev/hopbox/gen/hopbox/provider/v1"
	"github.com/hopboxdev/hopbox/internal/plugin/server"
	"github.com/hopboxdev/hopbox/providers/ingress/subdomain"
)

func main() {
	fs := flag.NewFlagSet("hopbox-provider", flag.ExitOnError)
	addr := fs.String("listen", ":9090", "gRPC listen address")
	kind := fs.String("serve", "compute", "what to serve: compute|storage|ingress")
	advertise := fs.String("advertise", "", "advertise address passed to the compute provider")
	zone := fs.String("zone", "gw.example.com", "wildcard DNS zone for the subdomain ingress provider")
	_ = fs.Parse(os.Args[1:])

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("hopbox-provider: listen: %v", err)
	}
	gs := grpc.NewServer()
	switch *kind {
	case "compute":
		c, err := newCompute(*advertise) // build-tagged: real provider or stub error
		if err != nil {
			log.Fatalf("hopbox-provider: compute: %v", err)
		}
		pb.RegisterComputeServer(gs, server.NewCompute(c))
	case "storage":
		pb.RegisterStorageServer(gs, server.NewStorage(newStorage()))
	case "ingress":
		pb.RegisterIngressServer(gs, server.NewIngress(subdomain.New(*zone)))
	default:
		log.Fatalf("hopbox-provider: unknown --serve %q", *kind)
	}
	log.Printf("hopbox-provider: serving %s on %s", *kind, *addr)
	if err := gs.Serve(ln); err != nil {
		log.Fatal(err)
	}
}
