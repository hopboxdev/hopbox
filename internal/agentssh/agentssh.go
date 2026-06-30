// Package agentssh runs an SSH server over a single already-established byte
// pipe (a yamux stream from hopboxd). The agent owns no listener and never binds
// a public port: the SSH transport rides the same reverse connection the agent
// dialed out on. A session offers an interactive pty shell, non-interactive
// exec, and the sftp subsystem (so VS Code Remote-SSH, scp and rsync all work).
package agentssh

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Config configures one SSH server connection.
type Config struct {
	HostKey        ssh.Signer      // server host key (presented to clients)
	TrustedUserCA  ssh.PublicKey   // accept user certs signed by this CA (the multi-user model)
	Principal      string          // the workspace owner; the SSH username must equal it
	AuthorizedKeys []ssh.PublicKey // fallback: a client key must match one of these (single-user / no login)
	Shell          string          // login shell; "" => $SHELL, /bin/bash, or /bin/sh
	HomeDir        string          // working dir + $HOME for shells/exec/sftp; "" => the agent's cwd
	Trusted        bool            // accept with no client auth — the caller (boxd front door) already authenticated the user and the stream is control-plane-only
}

// Serve runs the SSH protocol over rwc until the connection closes. rwc is
// typically a *yamux.Stream (already a net.Conn); anything else is adapted.
func Serve(rwc io.ReadWriteCloser, cfg Config) error {
	if cfg.HostKey == nil {
		return errors.New("agentssh: nil host key")
	}
	nc, ok := rwc.(net.Conn)
	if !ok {
		nc = &rwcConn{rwc}
	}

	// CA-signed user certificates: trust one CA, and require the cert to name the
	// workspace owner as a principal — so every box can trust the same CA yet only
	// its owner's certs open it.
	checker := &ssh.CertChecker{
		IsUserAuthority: func(auth ssh.PublicKey) bool {
			return cfg.TrustedUserCA != nil && keyEqual(auth.Marshal(), cfg.TrustedUserCA.Marshal())
		},
	}
	sc := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if cfg.TrustedUserCA != nil {
				if _, ok := key.(*ssh.Certificate); ok {
					if cfg.Principal != "" && conn.User() != cfg.Principal {
						return nil, fmt.Errorf("agentssh: %q is not the workspace owner", conn.User())
					}
					return checker.Authenticate(conn, key) // validates CA, type, expiry, principal
				}
			}
			// fallback: static authorized_keys (single-user / no login)
			km := key.Marshal()
			for _, ak := range cfg.AuthorizedKeys {
				if keyEqual(km, ak.Marshal()) {
					return &ssh.Permissions{}, nil
				}
			}
			return nil, fmt.Errorf("agentssh: unauthorized credential")
		},
	}
	if cfg.Trusted {
		// The boxd front door terminates the client's SSH, authenticates the user
		// (key fingerprint = identity), then proxies the session here over the
		// control-plane-only KindSSH stream — so this hop needs no further auth.
		sc.NoClientAuth = true
	}
	sc.AddHostKey(cfg.HostKey)

	conn, chans, reqs, err := ssh.NewServerConn(nc, sc)
	if err != nil {
		return fmt.Errorf("agentssh: handshake: %w", err)
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs) // we don't honour global requests (no -R yet)

	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go handleSession(ch, chReqs, cfg)
	}
	return nil
}

// session request payloads (RFC 4254).
type ptyReq struct {
	Term                 string
	Cols, Rows, Wpx, Hpx uint32
	Modes                string
}
type winchReq struct{ Cols, Rows, Wpx, Hpx uint32 }
type execReq struct{ Command string }
type subsystemReq struct{ Name string }
type exitStatus struct{ Status uint32 }

func handleSession(ch ssh.Channel, reqs <-chan *ssh.Request, cfg Config) {
	var (
		mu      sync.Mutex
		ptmx    *os.File // set once a pty-backed process is running
		started bool
		hasPTY  bool
		cols    uint16 = 80
		rows    uint16 = 24
	)
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			var p ptyReq
			if err := ssh.Unmarshal(req.Payload, &p); err == nil {
				if p.Cols > 0 {
					cols, rows = uint16(p.Cols), uint16(p.Rows)
				}
				hasPTY = true
			}
			_ = req.Reply(true, nil)
		case "window-change":
			var w winchReq
			if err := ssh.Unmarshal(req.Payload, &w); err == nil {
				mu.Lock()
				if ptmx != nil {
					_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(w.Cols), Rows: uint16(w.Rows)})
				}
				cols, rows = uint16(w.Cols), uint16(w.Rows)
				mu.Unlock()
			}
		case "env":
			_ = req.Reply(true, nil) // accept and ignore
		case "shell", "exec":
			if started {
				_ = req.Reply(false, nil)
				continue
			}
			started = true
			cmdline := []string{loginShell(cfg.Shell)} // interactive login shell
			if req.Type == "exec" {
				var e execReq
				_ = ssh.Unmarshal(req.Payload, &e)
				cmdline = shellDashC(cfg.Shell, e.Command)
			}
			_ = req.Reply(true, nil)
			go runCommand(ch, cmdline, cfg.HomeDir, hasPTY, cols, rows, &mu, &ptmx)
		case "subsystem":
			var s subsystemReq
			_ = ssh.Unmarshal(req.Payload, &s)
			if started || s.Name != "sftp" {
				_ = req.Reply(false, nil)
				continue
			}
			started = true
			_ = req.Reply(true, nil)
			go runSFTP(ch, cfg.HomeDir)
		default:
			_ = req.Reply(false, nil)
		}
	}
}

// runCommand runs an interactive shell, a login shell, or `sh -c <cmd>` and
// bridges it to the channel, with a pty when one was requested.
func runCommand(ch ssh.Channel, cmdline []string, home string, hasPTY bool, cols, rows uint16, mu *sync.Mutex, ptmx **os.File) {
	cmd := exec.Command(cmdline[0], cmdline[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if home != "" {
		cmd.Dir = home
		cmd.Env = append(cmd.Env, "HOME="+home) // a later HOME= wins, so this overrides any inherited one
	}

	if hasPTY {
		f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
		if err != nil {
			fmt.Fprintf(ch.Stderr(), "hopbox: pty: %v\r\n", err)
			exitClose(ch, 1)
			return
		}
		mu.Lock()
		*ptmx = f
		mu.Unlock()
		go func() { _, _ = io.Copy(f, ch) }() // client -> pty
		_, _ = io.Copy(ch, f)                 // pty -> client
		err = cmd.Wait()
		_ = f.Close()
		exitClose(ch, exitCode(err))
		return
	}

	// no pty: wire stdio directly. Use a stdin pipe + an unwaited copy so the
	// process exiting doesn't block on the client closing its write side — rsync's
	// server can finish before the client half-closes stdin, and `cmd.Stdin = ch`
	// would make cmd.Wait hang on the stuck copy goroutine (deadlock).
	stdin, err := cmd.StdinPipe()
	if err != nil {
		exitClose(ch, 1)
		return
	}
	cmd.Stdout = ch
	cmd.Stderr = ch.Stderr()
	if err := cmd.Start(); err != nil {
		exitClose(ch, exitCode(err))
		return
	}
	go func() { _, _ = io.Copy(stdin, ch); _ = stdin.Close() }()
	exitClose(ch, exitCode(cmd.Wait()))
}

func runSFTP(ch ssh.Channel, home string) {
	var opts []sftp.ServerOption
	if home != "" {
		opts = append(opts, sftp.WithServerWorkingDirectory(home)) // relative paths resolve against ~
	}
	srv, err := sftp.NewServer(ch, opts...)
	if err != nil {
		exitClose(ch, 1)
		return
	}
	if err := srv.Serve(); err != nil && err != io.EOF {
		_ = srv.Close()
	}
	exitClose(ch, 0)
}

// exitClose sends the SSH exit-status then closes the channel.
func exitClose(ch ssh.Channel, code int) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(exitStatus{Status: uint32(code)}))
	_ = ch.Close()
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

// shellDashC turns a remote command string into argv: `<shell> -c <command>`,
// matching how OpenSSH dispatches `ssh host "cmd"`, scp and rsync.
func shellDashC(shell, command string) []string {
	return []string{loginShell(shell), "-c", command}
}

// loginShell resolves the shell to run: the configured one, else $SHELL, else
// the first of /bin/bash, /bin/sh that exists.
func loginShell(shell string) string {
	if shell != "" {
		return shell
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	for _, c := range []string{"/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "/bin/sh"
}

func keyEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// rwcConn adapts a plain pipe to net.Conn for ssh.NewServerConn, which only
// needs Read/Write/Close in practice; addrs and deadlines are no-ops.
type rwcConn struct{ io.ReadWriteCloser }

func (rwcConn) LocalAddr() net.Addr              { return pipeAddr{} }
func (rwcConn) RemoteAddr() net.Addr             { return pipeAddr{} }
func (rwcConn) SetDeadline(time.Time) error      { return nil }
func (rwcConn) SetReadDeadline(time.Time) error  { return nil }
func (rwcConn) SetWriteDeadline(time.Time) error { return nil }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "hopbox-agent" }
