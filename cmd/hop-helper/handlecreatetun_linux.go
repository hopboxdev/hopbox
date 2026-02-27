//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/hopboxdev/hopbox/internal/helper"
	"golang.org/x/sys/unix"
)

func handleCreateTUN(conn net.Conn, mtu int) {
	tunFile, ifName, err := helper.CreateTUNDevice(mtu)
	if err != nil {
		writeError(conn, fmt.Sprintf("create TUN: %v", err))
		return
	}
	defer func() { _ = tunFile.Close() }()

	resp := helper.Response{OK: true, Interface: ifName}
	respBytes, err := json.Marshal(resp)
	if err != nil {
		writeError(conn, fmt.Sprintf("marshal response: %v", err))
		return
	}

	// Send the JSON response + the TUN fd via SCM_RIGHTS.
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		writeError(conn, "internal: expected UnixConn")
		return
	}

	rights := unix.UnixRights(int(tunFile.Fd()))
	if _, _, err := unixConn.WriteMsgUnix(respBytes, rights, nil); err != nil {
		log.Printf("failed to send TUN fd: %v", err)
	}
}

func cleanupTUN(iface string) error {
	return helper.CleanupTUN(iface)
}
