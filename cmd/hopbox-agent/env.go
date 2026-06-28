package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
)

// applyPackedEnv decodes HOPBOX_ENV64 — a base64 JSON map of the full
// environment — into the process env. The microVM provider packs env there
// because kernel cmdline tokens can't carry spaces or newlines, which the
// dev-env's HOPBOX_TRUSTED_USER_CA and HOPBOX_AUTHORIZED_KEYS contain. Called
// first, before anything reads HOPBOX_*. A no-op on backends (docker) that set
// the environment directly.
func applyPackedEnv() {
	b64 := os.Getenv("HOPBOX_ENV64")
	if b64 == "" {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		log.Printf("hopbox-agent: HOPBOX_ENV64 decode: %v", err)
		return
	}
	var env map[string]string
	if err := json.Unmarshal(raw, &env); err != nil {
		log.Printf("hopbox-agent: HOPBOX_ENV64 parse: %v", err)
		return
	}
	for k, v := range env {
		_ = os.Setenv(k, v)
	}
}
