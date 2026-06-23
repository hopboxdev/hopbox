package main

import (
	"io"
	"log"
	"os"

	"github.com/hopboxdev/hopbox/internal/agentssh"
)

// agentSSH holds the SSH server config (host key + authorized keys), loaded once
// at startup and reused for every KindSSH stream.
var agentSSH agentssh.Config

// loadSSHConfig prepares the agent's SSH server: a persistent host key and the
// set of authorized user keys. Authorized keys come from HOPBOX_AUTHORIZED_KEYS
// (an inline authorized_keys blob) or HOPBOX_AUTHORIZED_KEYS_FILE. With no keys
// the server still starts but rejects every client.
func loadSSHConfig() {
	keyPath := os.Getenv("HOPBOX_SSH_HOST_KEY")
	if keyPath == "" {
		keyPath = "/var/lib/hopbox/ssh_host_ed25519_key"
	}
	signer, err := agentssh.LoadOrCreateHostKey(keyPath)
	if err != nil {
		log.Printf("hopbox-agent: ssh host key: %v (ssh disabled)", err)
		return
	}
	agentSSH.HostKey = signer

	// CA model: trust one user CA, scoped to this workspace's owner.
	agentSSH.Principal = os.Getenv("HOPBOX_PRINCIPAL")
	if ca := os.Getenv("HOPBOX_TRUSTED_USER_CA"); ca != "" {
		if keys, err := agentssh.ParseAuthorizedKeys([]byte(ca)); err == nil {
			agentSSH.TrustedUserCA = keys[0]
		} else {
			log.Printf("hopbox-agent: trusted user CA: %v", err)
		}
	}

	var blob []byte
	if inline := os.Getenv("HOPBOX_AUTHORIZED_KEYS"); inline != "" {
		blob = []byte(inline)
	} else if f := os.Getenv("HOPBOX_AUTHORIZED_KEYS_FILE"); f != "" {
		if b, err := os.ReadFile(f); err == nil {
			blob = b
		}
	}
	if len(blob) > 0 {
		if keys, err := agentssh.ParseAuthorizedKeys(blob); err == nil {
			agentSSH.AuthorizedKeys = keys
		} else {
			log.Printf("hopbox-agent: authorized keys: %v", err)
		}
	}
	log.Printf("hopbox-agent: ssh ready (host key %s, CA=%t principal=%q, %d static keys)",
		keyPath, agentSSH.TrustedUserCA != nil, agentSSH.Principal, len(agentSSH.AuthorizedKeys))
}

// handleSSH serves the SSH protocol over a yamux stream (hopboxd bridges the
// client's `hopbox proxy` to it). Returns when the SSH connection closes.
func handleSSH(stream io.ReadWriteCloser) {
	if agentSSH.HostKey == nil {
		log.Printf("hopbox-agent: ssh requested but not configured")
		return
	}
	if err := agentssh.Serve(stream, agentSSH); err != nil {
		log.Printf("hopbox-agent: ssh serve: %v", err)
	}
}
