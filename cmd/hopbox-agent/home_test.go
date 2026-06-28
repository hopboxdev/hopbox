package main

import (
	"os"
	"testing"
)

// Without the home env (docker/k8s), mountHome is a no-op and never touches mount.
func TestMountHomeNoopWithoutEnv(t *testing.T) {
	os.Unsetenv("HOPBOX_HOME_DEV")
	os.Unsetenv("HOPBOX_HOME_MOUNT")
	mountHome() // must return immediately, no panic, no mount attempt
}
