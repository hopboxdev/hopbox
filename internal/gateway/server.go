package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/client"
	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/control"
	"github.com/hopboxdev/hopbox/internal/picker"
	"github.com/hopboxdev/hopbox/internal/users"
	"github.com/hopboxdev/hopbox/internal/wizard"
)

type Server struct {
	cfg       config.Config
	store     *users.Store
	manager   *containers.Manager
	dockerCli *client.Client
	baseTag   string
	linkStore *control.LinkStore
	sshSrv    *ssh.Server
}

func NewServer(cfg config.Config, store *users.Store, manager *containers.Manager, dockerCli *client.Client, baseTag string, linkStore *control.LinkStore) (*Server, error) {
	s := &Server{
		cfg:       cfg,
		store:     store,
		manager:   manager,
		dockerCli: dockerCli,
		baseTag:   baseTag,
		linkStore: linkStore,
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
			"direct-tcpip": DirectTCPIPHandler(s.manager, s.store, s.dockerCli, s.baseTag, s.cfg.Hostname, s.cfg.Port),
		},
	}

	s.sshSrv.AddHostKey(hostKey)

	return s, nil
}

func (s *Server) ListenAndServe() error {
	slog.Info("hopboxd listening", "port", s.cfg.Port)
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
		slog.Info("auth success", "component", "auth", "user", user.Username, "key_type", key.Type(), "addr", ctx.RemoteAddr())
		ctx.SetValue("needs_registration", false)
		return true
	}

	if s.cfg.OpenRegistration {
		slog.Info("auth new key, starting registration", "component", "auth", "addr", ctx.RemoteAddr(), "key_type", key.Type())
		ctx.SetValue("needs_registration", true)
		return true
	}

	slog.Warn("auth rejected: registration closed", "component", "auth", "addr", ctx.RemoteAddr())
	return false
}

func (s *Server) sessionHandler(sess ssh.Session) {
	ctx := sess.Context()
	fp := ctx.Value("fingerprint").(string)
	needsReg := ctx.Value("needs_registration").(bool)

	_, boxname := ParseUsername(sess.User())

	// Handle picker request (hop+?)
	if boxname == "?" {
		if needsReg {
			fmt.Fprintf(sess, "Please register first: ssh hop@server\r\n")
			sess.Exit(1)
			return
		}

		user, ok := s.store.LookupByFingerprint(fp)
		if !ok {
			fmt.Fprintf(sess, "User not found\r\n")
			return
		}

		userDir := filepath.Join(s.store.Dir(), fp)
		boxes, err := containers.ListBoxes(userDir)
		if err != nil {
			fmt.Fprintf(sess, "Failed to list boxes: %v\r\n", err)
			return
		}

		if len(boxes) == 0 {
			boxname = "default"
		} else {
			chosen, err := picker.RunPicker(boxes, sess)
			if err != nil {
				slog.Warn("picker cancelled", "component", "session", "user", user.Username, "err", err)
				sess.Exit(0)
				return
			}
			boxname = chosen
			slog.Info("box picked", "component", "session", "user", user.Username, "box", boxname)
		}
	}

	// Resolve user and profile
	userDir := filepath.Join(s.store.Dir(), fp)
	var user users.User
	var profile *users.Profile

	if !needsReg {
		var ok bool
		user, ok = s.store.LookupByFingerprint(fp)
		if !ok {
			slog.Warn("session user not found", "component", "session", "fp", fp[:20])
			fmt.Fprintf(sess, "User not found\r\n")
			return
		}

		slog.Info("session connect", "component", "session", "user", user.Username, "box", boxname)

		var err error
		profile, err = users.ResolveProfile(userDir, boxname)
		if err != nil {
			slog.Error("resolve profile failed", "component", "session", "err", err)
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
			slog.Warn("setup cancelled", "component", "session", "err", err)
			sess.Exit(0)
			return
		}

		// Handle link mode — user chose to link their key to an existing account
		if result.LinkMode {
			linkedUser, linkedBox, linkedProfile, linkErr := s.handleLinkFlow(sess, fp, result.LinkCode)
			if linkErr != nil {
				return // error already printed to session
			}
			user = linkedUser
			boxname = linkedBox
			profile = linkedProfile
			userDir = filepath.Join(s.store.Dir(), fp)
		} else if needsReg {
			// Save user if new registration
			u := users.User{
				Username:     result.Username,
				KeyType:      ctx.Value("key_type").(string),
				RegisteredAt: time.Now().UTC(),
			}
			if err := s.store.Save(fp, u); err != nil {
				slog.Error("save user failed", "component", "session", "err", err)
				fmt.Fprintf(sess, "Failed to save user: %v\r\n", err)
				return
			}
			user = u
			slog.Info("user registered", "component", "session", "user", user.Username, "fp", fp[:20])

			// Save as user-level default
			if err := users.SaveProfile(filepath.Join(userDir, "profile.toml"), result.Profile); err != nil {
				slog.Error("save user profile failed", "component", "session", "err", err)
			}

			// Save box-level profile
			boxDir := filepath.Join(userDir, "boxes", boxname)
			if err := os.MkdirAll(boxDir, 0755); err != nil {
				slog.Error("create box dir failed", "component", "session", "err", err)
			}
			if err := users.SaveProfile(filepath.Join(boxDir, "profile.toml"), result.Profile); err != nil {
				slog.Error("save box profile failed", "component", "session", "err", err)
			}
			profile = &result.Profile
		} else {
			// Existing user, new box — save box-level profile
			boxDir := filepath.Join(userDir, "boxes", boxname)
			if err := os.MkdirAll(boxDir, 0755); err != nil {
				slog.Error("create box dir failed", "component", "session", "err", err)
			}
			if err := users.SaveProfile(filepath.Join(boxDir, "profile.toml"), result.Profile); err != nil {
				slog.Error("save box profile failed", "component", "session", "err", err)
			}
			profile = &result.Profile
		}
	}

	// Ensure per-user image exists (with spinner)
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	buildDone := make(chan struct{})
	go func() {
		i := 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-buildDone:
				return
			case <-ticker.C:
				fmt.Fprintf(sess, "\r%s Building environment...", spinner[i%len(spinner)])
				i++
			}
		}
	}()
	imageTag, err := containers.EnsureUserImage(ctx, s.dockerCli, user.Username, *profile, s.baseTag)
	close(buildDone)
	if err != nil {
		fmt.Fprintf(sess, "\r\x1b[K✗ Building environment failed\r\n")
		slog.Error("build image failed", "component", "session", "err", err)
		fmt.Fprintf(sess, "  %v\r\n", err)
		return
	}
	fmt.Fprintf(sess, "\r\x1b[K✓ Environment ready!\r\n")

	// Container lifecycle
	homePath := s.store.HomePath(fp, boxname)
	if err := os.MkdirAll(homePath, 0755); err != nil {
		slog.Error("create home dir failed", "component", "session", "err", err)
		fmt.Fprintf(sess, "Failed to create home directory: %v\r\n", err)
		return
	}

	profileHash := profile.Hash()
	boxInfo := control.BoxInfo{
		BoxName:     boxname,
		Username:    user.Username,
		Shell:       profile.Shell.Tool,
		Multiplexer: profile.Multiplexer.Tool,
		Hostname:    s.cfg.Hostname,
		SSHPort:     s.cfg.Port,
		Fingerprint: fp,
	}
	containerID, err := s.manager.EnsureRunning(ctx, user.Username, boxname, imageTag, profileHash, homePath, boxInfo)
	if err != nil {
		slog.Error("container failed", "component", "session", "user", user.Username, "box", boxname, "err", err)
		fmt.Fprintf(sess, "Failed to start container: %v\r\n", err)
		return
	}
	ctx.SetValue("container_id", containerID)
	s.manager.SessionConnect(containerID)
	defer s.manager.SessionDisconnect(containerID)

	slog.Info("session attached", "component", "session", "user", user.Username, "box", boxname, "container", containerID[:12])

	// Get PTY for container exec (wizard already consumed resize events during its run)
	ptyReq, winCh, isPty := sess.Pty()
	if !isPty {
		slog.Warn("session no PTY", "component", "session", "user", user.Username)
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
	default:
		muxCmd = shellBin
	}

	shellCmd := fmt.Sprintf(
		`if ! infocmp %s >/dev/null 2>&1; then export TERM=xterm-256color; else export TERM=%s; fi; export SHELL=%s; exec %s`,
		term, term, shellBin, muxCmd,
	)
	cmd := []string{"bash", "-c", shellCmd}
	env := []string{fmt.Sprintf("TERM=%s", term), fmt.Sprintf("SHELL=%s", shellBin)}

	if err := s.manager.Exec(ctx, containerID, cmd, env, sess, sess, resizeCh); err != nil {
		slog.Error("exec error", "component", "session", "user", user.Username, "err", err)
		fmt.Fprintf(sess, "Session error: %v\r\n", err)
	}

	slog.Info("session disconnect", "component", "session", "user", user.Username, "box", boxname)
	sess.Exit(0)
}

func (s *Server) handleLinkFlow(sess ssh.Session, fp, code string) (users.User, string, *users.Profile, error) {
	originalFP, err := s.linkStore.ValidateCode(code)
	if err != nil {
		slog.Warn("link code validation failed", "component", "session", "err", err)
		fmt.Fprintf(sess, "Invalid or expired link code.\r\n")
		sess.Exit(1)
		return users.User{}, "", nil, err
	}

	if err := s.store.LinkKey(fp, originalFP); err != nil {
		slog.Error("link key failed", "component", "session", "err", err)
		fmt.Fprintf(sess, "Failed to link key: %v\r\n", err)
		sess.Exit(1)
		return users.User{}, "", nil, err
	}

	user, ok := s.store.LookupByFingerprint(fp)
	if !ok {
		fmt.Fprintf(sess, "User not found after linking\r\n")
		sess.Exit(1)
		return users.User{}, "", nil, fmt.Errorf("user not found after linking")
	}
	slog.Info("key linked", "component", "session", "fp", fp[:20], "user", user.Username)

	userDir := filepath.Join(s.store.Dir(), originalFP)
	boxes, err := containers.ListBoxes(userDir)
	if err != nil {
		fmt.Fprintf(sess, "Failed to list boxes: %v\r\n", err)
		sess.Exit(1)
		return users.User{}, "", nil, err
	}

	var boxname string
	if len(boxes) == 0 {
		boxname = "default"
	} else if len(boxes) == 1 {
		boxname = boxes[0]
	} else {
		chosen, err := picker.RunPicker(boxes, sess)
		if err != nil {
			slog.Warn("picker cancelled", "component", "session", "user", user.Username, "err", err)
			sess.Exit(0)
			return users.User{}, "", nil, err
		}
		boxname = chosen
	}

	profile, err := users.ResolveProfile(userDir, boxname)
	if err != nil {
		fmt.Fprintf(sess, "Failed to load profile: %v\r\n", err)
		return users.User{}, "", nil, err
	}
	if profile == nil {
		p := users.DefaultProfile()
		profile = &p
	}

	return user, boxname, profile, nil
}

func (s *Server) loadOrGenerateHostKey() (gossh.Signer, error) {
	keyPath := s.cfg.HostKeyPath

	if keyPath == "" {
		keyPath = filepath.Join(s.cfg.DataDir, "host_key")

		if _, err := os.Stat(keyPath); err == nil {
			return loadHostKey(keyPath)
		}

		slog.Warn("no host key configured, auto-generating", "path", keyPath)
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
