package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/users"
)

type Server struct {
	cfg      config.Config
	store    *users.Store
	manager  *containers.Manager
	imageTag string
	sshSrv   *ssh.Server
}

func NewServer(cfg config.Config, store *users.Store, manager *containers.Manager, imageTag string) (*Server, error) {
	s := &Server{
		cfg:      cfg,
		store:    store,
		manager:  manager,
		imageTag: imageTag,
	}

	hostKey, err := s.loadOrGenerateHostKey()
	if err != nil {
		return nil, fmt.Errorf("host key: %w", err)
	}

	s.sshSrv = &ssh.Server{
		Addr:             fmt.Sprintf(":%d", cfg.Port),
		PublicKeyHandler: s.authHandler,
		Handler:          s.sessionHandler,
		LocalPortForwardingCallback: func(ctx ssh.Context, destinationHost string, destinationPort uint32) bool {
			return true
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"session":      ssh.DefaultSessionHandler,
			"direct-tcpip": DirectTCPIPHandler(s.manager),
		},
	}

	s.sshSrv.AddHostKey(hostKey)

	return s, nil
}

func (s *Server) ListenAndServe() error {
	log.Printf("hopboxd listening on :%d", s.cfg.Port)
	return s.sshSrv.ListenAndServe()
}

func (s *Server) Close() error {
	return s.sshSrv.Close()
}

func (s *Server) authHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	fp := users.FormatFingerprint(gossh.FingerprintSHA256(key))
	ctx.SetValue("fingerprint", fp)
	ctx.SetValue("key_type", key.Type())

	user, known := s.store.LookupByFingerprint(fp)
	if known {
		log.Printf("[auth] user=%s key=%s addr=%s", user.Username, key.Type(), ctx.RemoteAddr())
		ctx.SetValue("needs_registration", false)
		return true
	}

	if s.cfg.OpenRegistration {
		log.Printf("[auth] new key addr=%s type=%s — starting registration", ctx.RemoteAddr(), key.Type())
		ctx.SetValue("needs_registration", true)
		return true
	}

	log.Printf("[auth] rejected unknown key addr=%s (registration closed)", ctx.RemoteAddr())
	return false
}

func (s *Server) sessionHandler(sess ssh.Session) {
	ctx := sess.Context()
	fp := ctx.Value("fingerprint").(string)
	needsReg := ctx.Value("needs_registration").(bool)

	_, boxname := ParseUsername(sess.User())

	if needsReg {
		username, err := users.RunRegistration(s.store, sess, sess)
		if err != nil {
			log.Printf("[session] registration failed addr=%s: %v", sess.RemoteAddr(), err)
			fmt.Fprintf(sess, "Registration failed: %v\r\n", err)
			return
		}

		u := users.User{
			Username:     username,
			KeyType:      ctx.Value("key_type").(string),
			RegisteredAt: time.Now().UTC(),
		}
		if err := s.store.Save(fp, u); err != nil {
			log.Printf("[session] save user failed: %v", err)
			fmt.Fprintf(sess, "Failed to save user: %v\r\n", err)
			return
		}

		log.Printf("[session] registered user=%s fp=%s", username, fp[:20])
		fmt.Fprintf(sess, "Welcome, %s! Setting up your dev environment...\r\n", username)
	}

	user, ok := s.store.LookupByFingerprint(fp)
	if !ok {
		log.Printf("[session] user not found for fp=%s", fp[:20])
		fmt.Fprintf(sess, "User not found\r\n")
		return
	}

	log.Printf("[session] connect user=%s box=%s", user.Username, boxname)

	homePath := s.store.HomePath(fp)
	containerID, err := s.manager.EnsureRunning(ctx, user.Username, boxname, s.imageTag, homePath)
	if err != nil {
		log.Printf("[session] container failed user=%s box=%s: %v", user.Username, boxname, err)
		fmt.Fprintf(sess, "Failed to start container: %v\r\n", err)
		return
	}
	ctx.SetValue("container_id", containerID)

	log.Printf("[session] attached user=%s box=%s container=%s", user.Username, boxname, containerID[:12])

	ptyReq, winCh, isPty := sess.Pty()
	if !isPty {
		log.Printf("[session] no PTY user=%s", user.Username)
		fmt.Fprintf(sess, "PTY required. Use: ssh -t ...\r\n")
		return
	}

	resizeCh := make(chan [2]uint, 1)
	resizeCh <- [2]uint{uint(ptyReq.Window.Width), uint(ptyReq.Window.Height)}

	go func() {
		for win := range winCh {
			resizeCh <- [2]uint{uint(win.Width), uint(win.Height)}
		}
		close(resizeCh)
	}()

	term := ptyReq.Term
	if term == "" {
		term = "xterm-256color"
	}
	// Fall back to xterm-256color if the container lacks the requested terminfo
	shellCmd := fmt.Sprintf(
		`if ! infocmp %s >/dev/null 2>&1; then export TERM=xterm-256color; else export TERM=%s; fi; exec zellij attach --create default`,
		term, term,
	)
	cmd := []string{"bash", "-c", shellCmd}
	env := []string{fmt.Sprintf("TERM=%s", term)}
	if err := s.manager.Exec(ctx, containerID, cmd, env, sess, sess, resizeCh); err != nil {
		log.Printf("[session] exec error user=%s: %v", user.Username, err)
		fmt.Fprintf(sess, "Session error: %v\r\n", err)
	}

	log.Printf("[session] disconnect user=%s box=%s", user.Username, boxname)
	sess.Exit(0)
}

func (s *Server) loadOrGenerateHostKey() (gossh.Signer, error) {
	keyPath := s.cfg.HostKeyPath

	if keyPath == "" {
		keyPath = filepath.Join(s.cfg.DataDir, "host_key")

		if _, err := os.Stat(keyPath); err == nil {
			return loadHostKey(keyPath)
		}

		log.Printf("WARNING: No host key configured, auto-generating to %s", keyPath)
		if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
			return nil, err
		}
		return generateAndSaveHostKey(keyPath)
	}

	return loadHostKey(keyPath)
}

func loadHostKey(path string) (gossh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read host key %s: %w", path, err)
	}
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse host key: %w", err)
	}
	return signer, nil
}

func generateAndSaveHostKey(path string) (gossh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	privBytes, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(path, pem.EncodeToMemory(privBytes), 0600); err != nil {
		return nil, err
	}

	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	return signer, nil
}
