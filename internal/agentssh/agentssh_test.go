package agentssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// serve runs agentssh.Serve behind a loopback TCP listener (OS-buffered, so the
// simultaneous KEXINIT exchange doesn't deadlock the way net.Pipe would) and
// returns the dial address + the user signer the server authorizes.
func serve(t *testing.T) (addr string, hostKey, userSigner ssh.Signer) {
	t.Helper()
	hostKey = mustSigner(t)
	userSigner = mustSigner(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go Serve(c, Config{
				HostKey:        hostKey,
				AuthorizedKeys: []ssh.PublicKey{userSigner.PublicKey()},
				Shell:          "/bin/sh",
			})
		}
	}()
	return ln.Addr().String(), hostKey, userSigner
}

func newClient(t *testing.T) *ssh.Client {
	t.Helper()
	addr, hostKey, userSigner := serve(t)
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "dev",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(userSigner)},
		HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestExec(t *testing.T) {
	client := newClient(t)
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	out, err := sess.Output("echo hopbox-works")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "hopbox-works" {
		t.Fatalf("exec output = %q, want hopbox-works", got)
	}
}

func TestExecExitCode(t *testing.T) {
	client := newClient(t)
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	err = sess.Run("exit 7")
	ee, ok := err.(*ssh.ExitError)
	if !ok {
		t.Fatalf("want *ssh.ExitError, got %T (%v)", err, err)
	}
	if ee.ExitStatus() != 7 {
		t.Fatalf("exit status = %d, want 7", ee.ExitStatus())
	}
}

func TestPTYShell(t *testing.T) {
	client := newClient(t)
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := sess.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatalf("pty: %v", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	io.WriteString(stdin, "echo marker-7842; exit\n")

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(stdout)
		done <- string(b)
	}()
	select {
	case got := <-done:
		if !strings.Contains(got, "marker-7842") {
			t.Fatalf("pty shell output missing marker: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for pty shell output")
	}
}

func TestSFTP(t *testing.T) {
	client := newClient(t)
	sftpc, err := sftp.NewClient(client)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	defer sftpc.Close()

	dir := t.TempDir()
	path := dir + "/hello.txt"
	f, err := sftpc.Create(path)
	if err != nil {
		t.Fatalf("sftp create: %v", err)
	}
	if _, err := f.Write([]byte("via-sftp")); err != nil {
		t.Fatalf("sftp write: %v", err)
	}
	f.Close()

	rf, err := sftpc.Open(path)
	if err != nil {
		t.Fatalf("sftp open: %v", err)
	}
	defer rf.Close()
	b, _ := io.ReadAll(rf)
	if string(b) != "via-sftp" {
		t.Fatalf("sftp readback = %q, want via-sftp", b)
	}
}

func TestUnauthorizedKeyRejected(t *testing.T) {
	hostKey := mustSigner(t)
	authorized := mustSigner(t)
	attacker := mustSigner(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = Serve(c, Config{HostKey: hostKey, AuthorizedKeys: []ssh.PublicKey{authorized.PublicKey()}})
	}()
	_, err = ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{
		User:            "dev",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(attacker)},
		HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()),
		Timeout:         5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected handshake to fail for an unauthorized key")
	}
}

func mustSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromSigner(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
