package main

import (
	"log"
	"os"
	"strings"
)

// ensureDNS writes a working /etc/resolv.conf from HOPBOX_DNS (space/comma-
// separated nameservers), set by the microVM provider. A microVM guest gets its
// IP + gateway from the kernel ip= arg but no DNS, and the catalog images ship a
// resolv.conf symlinked to a systemd-resolved stub (127.0.0.53) that isn't running
// in the box — so every hostname lookup fails (no git clone / apt / pip / curl).
// Replacing the file also drops any search domain the image build leaked from its
// host. A no-op on docker/k8s (HOPBOX_DNS unset), where the runtime owns DNS.
func ensureDNS() {
	spec := os.Getenv("HOPBOX_DNS")
	if spec == "" {
		return
	}
	var b strings.Builder
	for _, ns := range strings.FieldsFunc(spec, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n'
	}) {
		b.WriteString("nameserver " + ns + "\n")
	}
	if b.Len() == 0 {
		return
	}
	// The image usually symlinks resolv.conf to a stub; remove it so we write a
	// real file rather than following a dangling/managed link.
	_ = os.Remove("/etc/resolv.conf")
	if err := os.WriteFile("/etc/resolv.conf", []byte(b.String()), 0o644); err != nil {
		log.Printf("hopbox-agent: write resolv.conf: %v", err)
		return
	}
	log.Printf("hopbox-agent: DNS resolvers set (%s)", spec)
}
