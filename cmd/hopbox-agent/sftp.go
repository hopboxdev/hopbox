package main

import (
	"io"
	"log"

	"github.com/pkg/sftp"
)

// handleSFTP serves the SFTP protocol directly on a yamux stream. The boxd front
// door bridges a client's `sftp` subsystem channel to this stream (KindSFTP), so
// scp / sftp / rsync work against a box even though the front door itself only
// speaks shells + exec. Serves the box filesystem with the agent's privileges
// (root in the microVM), matching an interactive shell.
func handleSFTP(stream io.ReadWriteCloser) {
	// Resolve relative paths against the box's home (where shells land), so
	// `scp file box:f` lands in ~ rather than at the filesystem root.
	srv, err := sftp.NewServer(stream, sftp.WithServerWorkingDirectory(workspaceHome()))
	if err != nil {
		log.Printf("hopbox-agent: sftp server: %v", err)
		return
	}
	if err := srv.Serve(); err != nil && err != io.EOF {
		_ = srv.Close()
	}
}
