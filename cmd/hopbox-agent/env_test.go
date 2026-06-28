package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

// A space-containing env value (the dev-env's SSH CA) survives the HOPBOX_ENV64
// channel — the kernel cmdline can't carry it, so the microVM packs it here.
func TestApplyPackedEnv(t *testing.T) {
	ca := "ssh-ed25519 AAAA bob@host"
	blob, _ := json.Marshal(map[string]string{
		"HOPBOX_TRUSTED_USER_CA": ca,
		"HOPBOX_AGENT_TOKEN":     "tok",
	})
	t.Setenv("HOPBOX_ENV64", base64.StdEncoding.EncodeToString(blob))
	t.Setenv("HOPBOX_TRUSTED_USER_CA", "")
	t.Setenv("HOPBOX_AGENT_TOKEN", "")

	applyPackedEnv()

	if got := os.Getenv("HOPBOX_TRUSTED_USER_CA"); got != ca {
		t.Fatalf("CA=%q want %q (space-containing value must survive)", got, ca)
	}
	if got := os.Getenv("HOPBOX_AGENT_TOKEN"); got != "tok" {
		t.Fatalf("token=%q want tok", got)
	}
}
