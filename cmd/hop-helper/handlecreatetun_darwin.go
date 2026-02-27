//go:build darwin

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
	tunFile, err := helper.CreateTUNSocket()
	if err != nil {
		writeError(conn, fmt.Sprintf("create utun: %v", err))
		return
	}
	defer func() { _ = tunFile.Close() }()

	// Discover the interface name from the fd.
	ifName, err := unix.GetsockoptString(int(tunFile.Fd()), 2, 2) // SYSPROTO_CONTROL, UTUN_OPT_IFNAME
	if err != nil {
		writeError(conn, fmt.Sprintf("get utun name: %v", err))
		return
	}

	// Set MTU while we still have root privileges.
	if err := helper.SetMTU(ifName, mtu); err != nil {
		writeError(conn, fmt.Sprintf("set MTU: %v", err))
		return
	}

	resp := helper.Response{OK: true, Interface: ifName}
	respBytes, err := json.Marshal(resp)
	if err != nil {
		writeError(conn, fmt.Sprintf("marshal response: %v", err))
		return
	}

	// Send the JSON response + the utun fd via SCM_RIGHTS.
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		writeError(conn, "internal: expected UnixConn")
		return
	}

	rights := unix.UnixRights(int(tunFile.Fd()))
	if _, _, err := unixConn.WriteMsgUnix(respBytes, rights, nil); err != nil {
		log.Printf("failed to send utun fd: %v", err)
	}
}

func cleanupTUN(_ string) error {
	return helper.CleanupTUN()
}
