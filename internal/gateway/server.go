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

	"github.com/docker/docker/client"
	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/users"
	"github.com/hopboxdev/hopbox/internal/wizard"
)

type Server struct {
	cfg       config.Config
	store     *users.Store
	manager   *containers.Manager
	dockerCli *client.Client
	baseTag   string
	sshSrv    *ssh.Server
}

func NewServer(cfg config.Config, store *users.Store, manager *containers.Manager, dockerCli *client.Client, baseTag string) (*Server, error) {
	s := &Server{
		cfg:       cfg,
		store:     store,
		manager:   manager,
		dockerCli: dockerCli,
		baseTag:   baseTag,
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
			"direct-tcpip": DirectTCPIPHandler(s.manager, s.store, s.dockerCli, s.baseTag),
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

	// Resolve user and profile
	userDir := filepath.Join(s.store.Dir(), fp)
	var user users.User
	var profile *users.Profile

	if !needsReg {
		var ok bool
		user, ok = s.store.LookupByFingerprint(fp)
		if !ok {
			log.Printf("[session] user not found for fp=%s", fp[:20])
			fmt.Fprintf(sess, "User not found\r\n")
			return
		}

		log.Printf("[session] connect user=%s box=%s", user.Username, boxname)

		var err error
		profile, err = users.ResolveProfile(userDir, boxname)
		if err != nil {
			log.Printf("[session] resolve profile failed: %v", err)
			fmt.Fprintf(sess, "Failed to load profile: %v\r\n", err)
			return
		}
	}

	// Run wizard if new user or no profile exists
	if needsReg || profile == nil {
		defaults := users.DefaultProfile()
		if !needsReg {
			// Pre-fill with user's default for new box
			if userDefault, err := users.ResolveProfile(userDir, "__nonexistent__"); err == nil && userDefault != nil {
				defaults = *userDefault
			}
		}

		validateUsername := func(name string) error {
			if err := users.ValidateUsername(name); err != nil {
				return err
			}
			if s.store.IsUsernameTaken(name) {
				return fmt.Errorf("username %q is already taken", name)
			}
			return nil
		}

		result, err := wizard.RunSetup(defaults, sess, needsReg, validateUsername)
		if err != nil {
			log.Printf("[session] setup cancelled: %v", err)
			sess.Exit(0)
			return
		}

		// Save user if new registration
		if needsReg {
			u := users.User{
				Username:     result.Username,
				KeyType:      ctx.Value("key_type").(string),
				RegisteredAt: time.Now().UTC(),
			}
			if err := s.store.Save(fp, u); err != nil {
				log.Printf("[session] save user failed: %v", err)
				fmt.Fprintf(sess, "Failed to save user: %v\r\n", err)
				return
			}
			user = u
			log.Printf("[session] registered user=%s fp=%s", user.Username, fp[:20])

			// Save as user-level default
			if err := users.SaveProfile(filepath.Join(userDir, "profile.toml"), result.Profile); err != nil {
				log.Printf("[session] save user profile failed: %v", err)
			}
		}

		// Save box-level profile
		boxDir := filepath.Join(userDir, "boxes", boxname)
		os.MkdirAll(boxDir, 0755)
		if err := users.SaveProfile(filepath.Join(boxDir, "profile.toml"), result.Profile); err != nil {
			log.Printf("[session] save box profile failed: %v", err)
		}
		profile = &result.Profile
	}

	log.Printf("[session] connect user=%s box=%s", user.Username, boxname)

	// Ensure per-user image exists (with loading indicator)
	fmt.Fprintf(sess, "Building environment...")
	buildDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-buildDone:
				return
			case <-ticker.C:
				fmt.Fprintf(sess, ".")
			}
		}
	}()
	imageTag, err := containers.EnsureUserImage(ctx, s.dockerCli, user.Username, *profile, s.baseTag)
	close(buildDone)
	if err != nil {
		fmt.Fprintf(sess, "\r\n")
		log.Printf("[session] build image failed: %v", err)
		fmt.Fprintf(sess, "Failed to build environment: %v\r\n", err)
		return
	}
	fmt.Fprintf(sess, " done!\r\n")

	// Container lifecycle
	homePath := s.store.HomePath(fp, boxname)
	if err := os.MkdirAll(homePath, 0755); err != nil {
		log.Printf("[session] create home dir failed: %v", err)
		fmt.Fprintf(sess, "Failed to create home directory: %v\r\n", err)
		return
	}

	profileHash := profile.Hash()
	containerID, err := s.manager.EnsureRunning(ctx, user.Username, boxname, imageTag, profileHash, homePath)
	if err != nil {
		log.Printf("[session] container failed user=%s box=%s: %v", user.Username, boxname, err)
		fmt.Fprintf(sess, "Failed to start container: %v\r\n", err)
		return
	}
	ctx.SetValue("container_id", containerID)

	log.Printf("[session] attached user=%s box=%s container=%s", user.Username, boxname, containerID[:12])

	// Get PTY for container exec (wizard already consumed resize events during its run)
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

	// Adaptive exec based on profile
	term := ptyReq.Term
	if term == "" {
		term = "xterm-256color"
	}

	shellBin := "/bin/bash"
	switch profile.Shell.Tool {
	case "zsh":
		shellBin = "/usr/bin/zsh"
	case "fish":
		shellBin = "/usr/bin/fish"
	}

	var muxCmd string
	switch profile.Multiplexer.Tool {
	case "zellij":
		muxCmd = "zellij attach --create default"
	case "tmux":
		muxCmd = "tmux new-session -As default"
	}

	shellCmd := fmt.Sprintf(
		`if ! infocmp %s >/dev/null 2>&1; then export TERM=xterm-256color; else export TERM=%s; fi; export SHELL=%s; exec %s`,
		term, term, shellBin, muxCmd,
	)
	cmd := []string{"bash", "-c", shellCmd}
	env := []string{fmt.Sprintf("TERM=%s", term), fmt.Sprintf("SHELL=%s", shellBin)}

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
