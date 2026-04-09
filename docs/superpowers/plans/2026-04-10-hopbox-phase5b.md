# Phase 5B Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let users link multiple SSH keys to the same account via one-time codes.

**Architecture:** A user inside a container runs `hopbox link` which sends a request through the control socket. The daemon generates a one-time `XXXX-XXXX` code stored in an in-memory `LinkStore` with a 5-minute TTL. From another machine, connecting with an unknown key triggers the wizard which now starts with a choice step: "Create new account" or "Link to existing account". Choosing link prompts for the code, validates it, creates a filesystem symlink from the new fingerprint to the original user directory, and connects directly to the user's boxes.

**Tech Stack:** Go, kong, charmbracelet/huh

**Spec:** `docs/superpowers/specs/2026-04-10-hopbox-phase5b-design.md`

---

## Task 1: Link Code Store

**Files:** `internal/control/linkcodes.go`, `internal/control/linkcodes_test.go`

Create a standalone `LinkStore` struct that generates and validates one-time link codes. Thread-safe with a mutex. Codes are `XXXX-XXXX` format (uppercase alphanumeric), stored with a 5-minute TTL, and consumed on validation.

### New file `internal/control/linkcodes.go`

```go
package control

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

const (
	codeTTL     = 5 * time.Minute
	codeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I to avoid confusion
)

// LinkCode associates a one-time code with a user's fingerprint.
type LinkCode struct {
	Fingerprint string
	ExpiresAt   time.Time
}

// LinkStore holds active link codes in memory.
type LinkStore struct {
	mu    sync.Mutex
	codes map[string]LinkCode // code -> LinkCode
}

// NewLinkStore creates an empty link store.
func NewLinkStore() *LinkStore {
	return &LinkStore{
		codes: make(map[string]LinkCode),
	}
}

// GenerateCode creates a new link code for the given fingerprint.
// Returns a code in XXXX-XXXX format.
func (s *LinkStore) GenerateCode(fingerprint string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	code, err := randomCode()
	if err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}

	s.codes[code] = LinkCode{
		Fingerprint: fingerprint,
		ExpiresAt:   time.Now().Add(codeTTL),
	}

	return code, nil
}

// ValidateCode checks that a code exists and has not expired, then consumes it.
// Returns the fingerprint associated with the code.
func (s *LinkStore) ValidateCode(code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	lc, ok := s.codes[code]
	if !ok {
		return "", fmt.Errorf("invalid or expired link code")
	}

	if time.Now().After(lc.ExpiresAt) {
		delete(s.codes, code)
		return "", fmt.Errorf("link code expired")
	}

	// Consume the code (one-time use)
	delete(s.codes, code)
	return lc.Fingerprint, nil
}

// randomCode generates a code in XXXX-XXXX format.
func randomCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	chars := make([]byte, 8)
	for i := range chars {
		chars[i] = codeCharset[int(b[i])%len(codeCharset)]
	}
	return fmt.Sprintf("%s-%s", string(chars[:4]), string(chars[4:])), nil
}
```

### New file `internal/control/linkcodes_test.go`

```go
package control

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateCode_Format(t *testing.T) {
	s := NewLinkStore()
	code, err := s.GenerateCode("SHA256_abc123")
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	parts := strings.Split(code, "-")
	if len(parts) != 2 {
		t.Fatalf("expected XXXX-XXXX format, got %q", code)
	}
	if len(parts[0]) != 4 || len(parts[1]) != 4 {
		t.Fatalf("expected 4-4 character parts, got %q", code)
	}

	// All characters should be uppercase alphanumeric from codeCharset
	for _, c := range strings.ReplaceAll(code, "-", "") {
		if !strings.ContainsRune(codeCharset, c) {
			t.Fatalf("unexpected character %q in code %q", string(c), code)
		}
	}
}

func TestValidateCode_Success(t *testing.T) {
	s := NewLinkStore()
	code, err := s.GenerateCode("SHA256_abc123")
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	fp, err := s.ValidateCode(code)
	if err != nil {
		t.Fatalf("ValidateCode: %v", err)
	}
	if fp != "SHA256_abc123" {
		t.Fatalf("expected fingerprint SHA256_abc123, got %q", fp)
	}
}

func TestValidateCode_ConsumedOnUse(t *testing.T) {
	s := NewLinkStore()
	code, err := s.GenerateCode("SHA256_abc123")
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	_, err = s.ValidateCode(code)
	if err != nil {
		t.Fatalf("first validate: %v", err)
	}

	_, err = s.ValidateCode(code)
	if err == nil {
		t.Fatal("expected error on second validate (code consumed)")
	}
}

func TestValidateCode_InvalidCode(t *testing.T) {
	s := NewLinkStore()
	_, err := s.ValidateCode("NOPE-NOPE")
	if err == nil {
		t.Fatal("expected error for invalid code")
	}
}

func TestValidateCode_Expired(t *testing.T) {
	s := NewLinkStore()
	code, err := s.GenerateCode("SHA256_abc123")
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	// Manually expire the code
	s.mu.Lock()
	lc := s.codes[code]
	lc.ExpiresAt = time.Now().Add(-1 * time.Minute)
	s.codes[code] = lc
	s.mu.Unlock()

	_, err = s.ValidateCode(code)
	if err == nil {
		t.Fatal("expected error for expired code")
	}
}
```

---

## Task 2: Add link command to control handler

**Files:** `internal/control/handler.go`, `internal/control/socket.go`

Add `Fingerprint` field to `BoxInfo`. Add `"link"` case to `HandleRequest` that uses a `LinkStore` to generate a code. Update `SocketServer` and `NewSocketServer` to accept and use a `*LinkStore`.

### Changes to `internal/control/handler.go`

Add `Fingerprint` to `BoxInfo`:

```go
type BoxInfo struct {
	BoxName     string
	Username    string
	Shell       string
	Multiplexer string
	ContainerID string
	StartedAt   time.Time
	Hostname    string
	SSHPort     int
	Fingerprint string
}
```

Change `HandleRequest` to accept a `*LinkStore` and add the `"link"` case:

```go
func HandleRequest(req Request, info BoxInfo, destroyFn DestroyFunc, linkStore *LinkStore) Response {
	switch req.Command {
	case "status":
		return handleStatus(info)
	case "destroy":
		return handleDestroy(req, info, destroyFn)
	case "link":
		return handleLink(info, linkStore)
	default:
		return Response{OK: false, Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}
}
```

Add the `handleLink` function:

```go
func handleLink(info BoxInfo, linkStore *LinkStore) Response {
	if linkStore == nil {
		return Response{OK: false, Error: "link codes not available"}
	}
	if info.Fingerprint == "" {
		return Response{OK: false, Error: "fingerprint not available"}
	}

	code, err := linkStore.GenerateCode(info.Fingerprint)
	if err != nil {
		return Response{OK: false, Error: fmt.Sprintf("generate link code: %v", err)}
	}

	return Response{OK: true, Data: map[string]string{"code": code}}
}
```

### Changes to `internal/control/socket.go`

Add `linkStore` field to `SocketServer` and update constructor:

```go
type SocketServer struct {
	path      string
	listener  net.Listener
	info      BoxInfo
	destroyFn DestroyFunc
	linkStore *LinkStore
	done      chan struct{}
	wg        sync.WaitGroup
}

func NewSocketServer(socketPath string, info BoxInfo, destroyFn DestroyFunc, linkStore *LinkStore) (*SocketServer, error) {
	// Remove stale socket file
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	// Make socket world-writable so the dev user inside the container can access it
	if err := os.Chmod(socketPath, 0666); err != nil {
		listener.Close()
		return nil, err
	}

	return &SocketServer{
		path:      socketPath,
		listener:  listener,
		info:      info,
		destroyFn: destroyFn,
		linkStore: linkStore,
		done:      make(chan struct{}),
	}, nil
}
```

Update `handleConn` to pass `linkStore`:

```go
func (s *SocketServer) handleConn(conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		resp := Response{OK: false, Error: "invalid request"}
		json.NewEncoder(conn).Encode(resp)
		return
	}

	resp := HandleRequest(req, s.info, s.destroyFn, s.linkStore)
	json.NewEncoder(conn).Encode(resp)
}
```

### Update callers of `NewSocketServer`

Find all call sites of `NewSocketServer` (in `internal/containers/manager.go`) and add the `linkStore` parameter. The Manager will need to hold a `*control.LinkStore` — see Task 6 for the full wiring.

For this task, update the Manager's `NewSocketServer` call to pass the link store. The Manager already receives `BoxInfo` via `EnsureRunning`, so add a `LinkStore` field:

```go
// In internal/containers/manager.go, add to Manager struct:
linkStore *control.LinkStore

// In NewManager (or equivalent constructor), accept *control.LinkStore and store it.

// In the EnsureRunning method where NewSocketServer is called, pass m.linkStore:
srv, err := control.NewSocketServer(socketPath, info, destroyFn, m.linkStore)
```

### Update callers of `HandleRequest`

Search for any direct callers of `HandleRequest` (currently only in `socket.go`'s `handleConn`) and update to pass `linkStore`. Also update any tests that call `HandleRequest` to pass `nil` for linkStore if they don't test link functionality.

---

## Task 3: Add `hopbox link` to CLI

**Files:** `cmd/hopbox/main.go`

Add a `LinkCmd` struct and wire it into the kong CLI. Sends `{"command": "link"}` via the control socket and prints the code.

### Changes to `cmd/hopbox/main.go`

Add `LinkCmd` to the CLI struct:

```go
type CLI struct {
	Status  StatusCmd  `cmd:"" help:"Show box info."`
	Expose  ExposeCmd  `cmd:"" help:"Print SSH tunnel instructions for a port."`
	Link    LinkCmd    `cmd:"" help:"Generate a link code to connect this account from another device."`
	Destroy DestroyCmd `cmd:"" help:"Destroy this box."`
}

type LinkCmd struct{}
```

Add the `"link"` case to `main()`:

```go
case "link":
	doLink()
```

Add the `doLink` function:

```go
func doLink() {
	resp, err := sendRequest(control.Request{Command: "link"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	code := resp.Data["code"]
	fmt.Printf("Link code: %s\n", code)
	fmt.Println("Expires in 5 minutes. On your other machine, connect and enter this code.")
}
```

---

## Task 4: Add stepChoice to wizard

**Files:** `internal/wizard/wizard.go`

Add `stepChoice` (and `stepLinkCode`) before `stepUsername`. When `needsRegistration` is true, the wizard starts at `stepChoice`. If the user picks "link", they enter a code and the wizard returns a special `Result` with `LinkMode` set. If they pick "create", the existing flow continues.

### Changes to `internal/wizard/wizard.go`

Update the step constants to add `stepChoice` and `stepLinkCode` at the beginning:

```go
const (
	stepChoice   step = iota
	stepLinkCode
	stepUsername
	stepMux
	stepEditor
	stepShell
	stepNode
	stepPython
	stepGo
	stepRust
	stepTools
	stepDone
)
```

Add `LinkMode` and `LinkCode` to `Result`:

```go
type Result struct {
	Username string
	Profile  users.Profile
	LinkMode bool   // true if user chose "link to existing account"
	LinkCode string // the code entered by the user
}
```

Add `Choice` and `LinkCode` to `wizardData`:

```go
type wizardData struct {
	Profile  users.Profile
	Username string
	Choice   string // "create" or "link"
	LinkCode string
}
```

Update the `Update` method to handle the choice-based step transitions. When `stepChoice` completes and the choice is "link", jump to `stepLinkCode`. When `stepLinkCode` completes, jump to `stepDone`. When `stepChoice` completes and the choice is "create", jump to `stepUsername`:

```go
func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Esc goes back one step
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyEscape && m.step > m.firstStep {
			m.step--
			// Skip stepLinkCode when going back if choice was "create"
			if m.step == stepLinkCode && m.data.Choice == "create" {
				m.step = stepChoice
			}
			m.form = m.buildForm(m.step)
			return m, m.form.Init()
		}
	}

	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
	}

	if m.form.State == huh.StateAborted {
		m.aborted = true
		return m, tea.Quit
	}

	if m.form.State == huh.StateCompleted {
		// Handle choice-based transitions
		if m.step == stepChoice {
			if m.data.Choice == "link" {
				m.step = stepLinkCode
			} else {
				m.step = stepUsername
			}
			m.form = m.buildForm(m.step)
			return m, m.form.Init()
		}

		if m.step == stepLinkCode {
			// Link code entered, we're done
			m.step = stepDone
			return m, tea.Quit
		}

		m.step++
		if m.step >= stepDone {
			return m, tea.Quit
		}
		m.form = m.buildForm(m.step)
		return m, m.form.Init()
	}

	return m, cmd
}
```

Add `stepChoice` and `stepLinkCode` cases to `buildForm`:

```go
case stepChoice:
	return huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Welcome to Hopbox!").
			Description("Choose how to get started.").
			Options(
				huh.NewOption("Create new account", "create"),
				huh.NewOption("Link to existing account", "link"),
			).Value(&m.data.Choice),
	))
case stepLinkCode:
	return huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Link Code").
			Description("Enter the code from `hopbox link` on your other device.").
			Placeholder("XXXX-XXXX").
			Value(&m.data.LinkCode).
			Validate(func(s string) error {
				if len(s) != 9 || s[4] != '-' {
					return fmt.Errorf("code must be in XXXX-XXXX format")
				}
				return nil
			}),
	))
```

Update `RunSetup` to start at `stepChoice` when `needsRegistration` is true, and to populate the result's link fields:

```go
func RunSetup(defaults users.Profile, sess ssh.Session, needsRegistration bool, validateUsername func(string) error) (*Result, error) {
	firstStep := stepMux
	if needsRegistration {
		firstStep = stepChoice
	}

	data := &wizardData{Profile: defaults}
	m := wizardModel{
		step:            firstStep,
		firstStep:       firstStep,
		data:            data,
		validateUsername: validateUsername,
	}
	m.form = m.buildForm(m.step)

	result, err := runProgram(sess, m)
	if err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}

	wm := result.(wizardModel)
	if wm.aborted {
		return nil, fmt.Errorf("setup cancelled")
	}
	return &Result{
		Username: data.Username,
		Profile:  data.Profile,
		LinkMode: data.Choice == "link",
		LinkCode: data.LinkCode,
	}, nil
}
```

---

## Task 5: Store.LinkKey method

**Files:** `internal/users/store.go`, `internal/users/store_test.go`

Add a `LinkKey` method that creates a filesystem symlink from the new fingerprint directory to the original one, then reloads the store so both fingerprints resolve to the same user.

### Changes to `internal/users/store.go`

Add the `LinkKey` method:

```go
// LinkKey creates a symlink so that newFP resolves to the same user as originalFP.
// After linking, both fingerprints will return the same User from LookupByFingerprint.
func (s *Store) LinkKey(newFP, originalFP string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify the original fingerprint exists
	if _, ok := s.users[originalFP]; !ok {
		return fmt.Errorf("original fingerprint not found: %s", originalFP)
	}

	newDir := filepath.Join(s.dir, newFP)
	originalDir := filepath.Join(s.dir, originalFP)

	// Check the new fingerprint dir doesn't already exist
	if _, err := os.Lstat(newDir); err == nil {
		return fmt.Errorf("fingerprint directory already exists: %s", newFP)
	}

	// Create the symlink: newFP -> originalFP (relative so it works if data dir moves)
	if err := os.Symlink(originalFP, newDir); err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}

	// Reload the user into the in-memory map for the new fingerprint
	user := s.users[originalFP]
	s.users[newFP] = user

	return nil
}
```

### New file `internal/users/store_test.go` (or add to existing test file)

Add tests for `LinkKey`. If the test file already exists, append these tests:

```go
package users

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLinkKey_BothFingerprintsResolve(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create a user
	originalFP := "SHA256_original"
	user := User{
		Username:     "testuser",
		KeyType:      "ssh-ed25519",
		RegisteredAt: time.Now().UTC(),
	}
	if err := store.Save(originalFP, user); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Link a new key
	newFP := "SHA256_newkey"
	if err := store.LinkKey(newFP, originalFP); err != nil {
		t.Fatalf("LinkKey: %v", err)
	}

	// Both fingerprints should resolve to the same user
	u1, ok := store.LookupByFingerprint(originalFP)
	if !ok {
		t.Fatal("original fingerprint not found")
	}
	u2, ok := store.LookupByFingerprint(newFP)
	if !ok {
		t.Fatal("new fingerprint not found after linking")
	}
	if u1.Username != u2.Username {
		t.Fatalf("usernames don't match: %q vs %q", u1.Username, u2.Username)
	}

	// Verify the symlink exists on disk
	linkPath := filepath.Join(dir, newFP)
	fi, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("Lstat symlink: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink, got regular file/dir")
	}
}

func TestLinkKey_OriginalNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	err := store.LinkKey("SHA256_new", "SHA256_nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent original fingerprint")
	}
}

func TestLinkKey_NewAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	fp1 := "SHA256_first"
	fp2 := "SHA256_second"
	user := User{
		Username:     "user1",
		KeyType:      "ssh-ed25519",
		RegisteredAt: time.Now().UTC(),
	}
	if err := store.Save(fp1, user); err != nil {
		t.Fatalf("Save fp1: %v", err)
	}

	user2 := User{
		Username:     "user2",
		KeyType:      "ssh-ed25519",
		RegisteredAt: time.Now().UTC(),
	}
	if err := store.Save(fp2, user2); err != nil {
		t.Fatalf("Save fp2: %v", err)
	}

	err := store.LinkKey(fp2, fp1)
	if err == nil {
		t.Fatal("expected error when new fingerprint dir already exists")
	}
}

func TestLinkKey_ReloadPicksUpLink(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	originalFP := "SHA256_orig"
	user := User{
		Username:     "reloaduser",
		KeyType:      "ssh-ed25519",
		RegisteredAt: time.Now().UTC(),
	}
	if err := store.Save(originalFP, user); err != nil {
		t.Fatalf("Save: %v", err)
	}

	newFP := "SHA256_linked"
	if err := store.LinkKey(newFP, originalFP); err != nil {
		t.Fatalf("LinkKey: %v", err)
	}

	// Create a fresh store from the same directory to test load picks up symlinks
	store2 := NewStore(dir)
	u, ok := store2.LookupByFingerprint(newFP)
	if !ok {
		t.Fatal("linked fingerprint not found after reload")
	}
	if u.Username != "reloaduser" {
		t.Fatalf("expected username reloaduser, got %q", u.Username)
	}
}
```

---

## Task 6: Wire link flow into session handler

**Files:** `internal/gateway/server.go`, `internal/gateway/tunnel.go`, `internal/containers/manager.go`

Pass the `LinkStore` through the `Server` struct so both the control socket (code generation) and the session handler (code validation) can use it. When the wizard returns `LinkMode`, validate the code, link the key, and connect the user.

### Changes to `internal/gateway/server.go`

Add `linkStore` to the `Server` struct and constructor:

```go
type Server struct {
	cfg       config.Config
	store     *users.Store
	manager   *containers.Manager
	dockerCli *client.Client
	baseTag   string
	linkStore *control.LinkStore
	sshSrv    *ssh.Server
}

func NewServer(cfg config.Config, store *users.Store, manager *containers.Manager, dockerCli *client.Client, baseTag string) (*Server, error) {
	linkStore := control.NewLinkStore()

	s := &Server{
		cfg:       cfg,
		store:     store,
		manager:   manager,
		dockerCli: dockerCli,
		baseTag:   baseTag,
		linkStore: linkStore,
	}
	// ... rest unchanged
```

Pass `linkStore` to `Manager` (or set it after construction):

```go
	// After creating the linkStore and manager, wire them together:
	manager.SetLinkStore(linkStore)
```

Update the `BoxInfo` construction in `sessionHandler` to include `Fingerprint`:

```go
	boxInfo := control.BoxInfo{
		BoxName:     boxname,
		Username:    user.Username,
		Shell:       profile.Shell.Tool,
		Multiplexer: profile.Multiplexer.Tool,
		Hostname:    s.cfg.Hostname,
		SSHPort:     s.cfg.Port,
		Fingerprint: fp,
	}
```

Add the link flow handling in `sessionHandler`. After the wizard returns, check if `LinkMode` is set. Insert this block right after `wizard.RunSetup` returns and before the existing `if needsReg` save block:

```go
		result, err := wizard.RunSetup(defaults, sess, needsReg, validateUsername)
		if err != nil {
			log.Printf("[session] setup cancelled: %v", err)
			sess.Exit(0)
			return
		}

		// Handle link mode — user chose to link their key to an existing account
		if result.LinkMode {
			originalFP, err := s.linkStore.ValidateCode(result.LinkCode)
			if err != nil {
				log.Printf("[session] link code validation failed: %v", err)
				fmt.Fprintf(sess, "Invalid or expired link code.\r\n")
				sess.Exit(1)
				return
			}

			if err := s.store.LinkKey(fp, originalFP); err != nil {
				log.Printf("[session] link key failed: %v", err)
				fmt.Fprintf(sess, "Failed to link key: %v\r\n", err)
				sess.Exit(1)
				return
			}

			// Look up the now-linked user
			user, ok = s.store.LookupByFingerprint(fp)
			if !ok {
				fmt.Fprintf(sess, "User not found after linking\r\n")
				sess.Exit(1)
				return
			}
			log.Printf("[session] linked fp=%s to user=%s", fp[:20], user.Username)

			// Resolve boxes for the linked user — use the original FP's directory
			userDir = filepath.Join(s.store.Dir(), originalFP)
			boxes, err := containers.ListBoxes(userDir)
			if err != nil {
				fmt.Fprintf(sess, "Failed to list boxes: %v\r\n", err)
				sess.Exit(1)
				return
			}

			if len(boxes) == 0 {
				boxname = "default"
			} else if len(boxes) == 1 {
				boxname = boxes[0]
			} else {
				chosen, err := picker.RunPicker(boxes, sess)
				if err != nil {
					log.Printf("[session] picker cancelled for user=%s: %v", user.Username, err)
					sess.Exit(0)
					return
				}
				boxname = chosen
			}

			// Resolve the profile for the chosen box
			profile, err = users.ResolveProfile(userDir, boxname)
			if err != nil {
				log.Printf("[session] resolve profile failed: %v", err)
				fmt.Fprintf(sess, "Failed to load profile: %v\r\n", err)
				return
			}
			if profile == nil {
				p := users.DefaultProfile()
				profile = &p
			}

			needsReg = false
			// Skip the rest of the wizard save logic, fall through to container lifecycle
			goto containerLifecycle
		}

		// Save user if new registration
		if needsReg {
			// ... existing save logic unchanged
```

Add the `containerLifecycle` label before the "Ensure per-user image exists" comment:

```go
containerLifecycle:
	// Ensure per-user image exists (with spinner)
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	// ... rest unchanged
```

**Note on goto:** If `goto` creates scoping issues with variable declarations, restructure the handler to extract the link flow into a helper method that returns the resolved `user`, `boxname`, `profile`, and a boolean indicating whether to proceed. This avoids the goto entirely:

```go
		if result.LinkMode {
			linkedUser, linkedBox, linkedProfile, err := s.handleLinkFlow(sess, fp, result.LinkCode)
			if err != nil {
				return // error already printed to session
			}
			user = linkedUser
			boxname = linkedBox
			profile = linkedProfile
			// Skip wizard save, fall through to container lifecycle
		} else if needsReg {
			// existing registration save logic
			// ...
		}
```

This helper approach is cleaner:

```go
func (s *Server) handleLinkFlow(sess ssh.Session, fp, code string) (users.User, string, *users.Profile, error) {
	originalFP, err := s.linkStore.ValidateCode(code)
	if err != nil {
		log.Printf("[session] link code validation failed: %v", err)
		fmt.Fprintf(sess, "Invalid or expired link code.\r\n")
		sess.Exit(1)
		return users.User{}, "", nil, err
	}

	if err := s.store.LinkKey(fp, originalFP); err != nil {
		log.Printf("[session] link key failed: %v", err)
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
	log.Printf("[session] linked fp=%s to user=%s", fp[:20], user.Username)

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
			log.Printf("[session] picker cancelled for user=%s: %v", user.Username, err)
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
```

### Changes to `internal/gateway/tunnel.go`

Update the `BoxInfo` construction in `resolveContainerID` to include `Fingerprint`:

```go
	boxInfo := control.BoxInfo{
		BoxName:     boxname,
		Username:    user.Username,
		Hostname:    hostname,
		SSHPort:     sshPort,
		Fingerprint: fp,
	}
```

### Changes to `internal/containers/manager.go`

Add a `LinkStore` field and setter to `Manager`:

```go
import "github.com/hopboxdev/hopbox/internal/control"

// Add to Manager struct:
linkStore *control.LinkStore

// Add setter method:
func (m *Manager) SetLinkStore(ls *control.LinkStore) {
	m.linkStore = ls
}
```

Update the `NewSocketServer` call in `EnsureRunning` to pass `m.linkStore`:

```go
srv, err := control.NewSocketServer(socketPath, info, destroyFn, m.linkStore)
```

---

## Task 7: Smoke test

**Manual verification steps:**

1. **Start hopboxd** with open registration enabled
2. **Connect from machine A** — register a new account (e.g. username "gandalf")
3. **Inside the container**, run `hopbox link` — note the code (e.g. `ABCD-1234`)
4. **Connect from machine B** (different SSH key) — the wizard shows "Create new account" / "Link to existing account"
5. **Choose "Link to existing account"** — enter the code
6. **Verify:** the new key is linked, user connects to gandalf's box
7. **Verify:** subsequent connections from machine B skip registration entirely
8. **Verify:** expired codes (wait 5 minutes) are rejected
9. **Verify:** reusing a consumed code is rejected

**Automated test to add** in `internal/control/linkcodes_test.go` (already covered in Task 1).

**Integration test sketch** (optional, can be added later):

```go
// TestLinkFlow_EndToEnd in internal/gateway/server_test.go
// 1. Create a Store with a user under fp1
// 2. Create a LinkStore, generate code for fp1
// 3. Validate code, get fp1 back
// 4. Call store.LinkKey(fp2, fp1)
// 5. Assert store.LookupByFingerprint(fp2) returns the same user
```
