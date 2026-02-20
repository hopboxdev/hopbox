package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hopboxdev/hopbox/internal/helper"
	"github.com/hopboxdev/hopbox/internal/version"
	"golang.org/x/sys/unix"
)

func main() {
	log.SetPrefix("hop-helper: ")
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// --install: copy self to /Library/PrivilegedHelperTools and install LaunchDaemon.
	if len(os.Args) == 2 && os.Args[1] == "--install" {
		if os.Geteuid() != 0 {
			log.Fatal("--install requires root (use sudo)")
		}
		if err := helper.Install(os.Args[0]); err != nil {
			log.Fatalf("install: %v", err)
		}
		fmt.Println("Helper daemon installed and started.")
		return
	}

	if os.Geteuid() != 0 {
		log.Fatal("must run as root")
	}

	sockDir := filepath.Dir(helper.SocketPath)
	if err := os.MkdirAll(sockDir, 0755); err != nil {
		log.Fatalf("create socket dir: %v", err)
	}
	// Remove stale socket.
	_ = os.Remove(helper.SocketPath)

	ln, err := net.Listen("unix", helper.SocketPath)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Allow non-root users to connect.
	if err := os.Chmod(helper.SocketPath, 0666); err != nil {
		log.Fatalf("chmod socket: %v", err)
	}

	log.Printf("listening on %s", helper.SocketPath)

	// Graceful shutdown.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("shutting down")
		_ = ln.Close()
		_ = os.Remove(helper.SocketPath)
		os.Exit(0)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(conn)
	}
}

func handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	var req helper.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeError(conn, fmt.Sprintf("decode request: %v", err))
		return
	}

	log.Printf("action=%s interface=%s hostname=%s", req.Action, req.Interface, req.Hostname)

	if req.Action == helper.ActionVersion {
		_ = json.NewEncoder(conn).Encode(helper.Response{OK: true, Version: version.Version})
		return
	}

	// CreateTUN is special: it needs to send the fd via SCM_RIGHTS.
	if req.Action == helper.ActionCreateTUN {
		handleCreateTUN(conn, req.MTU)
		return
	}

	var err error
	switch req.Action {
	case helper.ActionConfigureTUN:
		err = helper.ConfigureTUN(req.Interface, req.LocalIP, req.PeerIP)
	case helper.ActionCleanupTUN:
		err = helper.CleanupTUN()
	case helper.ActionAddHost:
		err = helper.AddHostEntry("/etc/hosts", req.IP, req.Hostname)
	case helper.ActionRemoveHost:
		err = helper.RemoveHostEntry("/etc/hosts", req.Hostname)
	default:
		err = fmt.Errorf("unknown action %q", req.Action)
	}

	if err != nil {
		writeError(conn, err.Error())
		return
	}
	_ = json.NewEncoder(conn).Encode(helper.Response{OK: true})
}

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

func writeError(conn net.Conn, msg string) {
	_ = json.NewEncoder(conn).Encode(helper.Response{OK: false, Error: msg})
}
