package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// newSSHConfigCmd writes a managed OpenSSH config entry for a workspace so that
// `ssh <alias>`, VS Code "Connect to Host", scp and rsync all reach it through
// `hopbox proxy` — no public port, no manual config. Entries live in
// ~/.ssh/hopbox/<alias>.config and are pulled in by an `Include hopbox/*.config`
// line added once to ~/.ssh/config.
func newSSHConfigCmd() *cobra.Command {
	var alias, user string
	c := &cobra.Command{
		Use:   "ssh-config <name|id>",
		Short: "Write an SSH config entry for a workspace (ssh / VS Code Remote-SSH)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if alias == "" {
				alias = name
			}
			if user == "" { // default to the principal from `hopbox login`
				if user = readPrincipal(); user == "" {
					user = "dev"
				}
			}
			self, err := os.Executable()
			if err != nil || self == "" {
				self = "hopbox" // fall back to PATH lookup
			}
			idPath, err := identityKeyPath()
			if err != nil {
				return err
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			sshDir := filepath.Join(home, ".ssh")
			if err := os.MkdirAll(filepath.Join(sshDir, "hopbox"), 0o700); err != nil {
				return err
			}
			if err := ensureInclude(filepath.Join(sshDir, "config")); err != nil {
				return err
			}

			block := fmt.Sprintf(`# managed by hopbox — workspace %q (regenerate with: hopbox ssh-config %s)
Host %s
    User %s
    IdentityFile %s
    IdentitiesOnly yes
    ProxyCommand %q proxy %s --addr %s
    StrictHostKeyChecking accept-new
    UserKnownHostsFile ~/.ssh/known_hosts
`, name, name, alias, user, idPath, self, name, apiAddr)

			path := filepath.Join(sshDir, "hopbox", alias+".config")
			if err := os.WriteFile(path, []byte(block), 0o600); err != nil {
				return err
			}
			fmt.Printf("wrote %s\n\nConnect:\n  ssh %s\n  code --remote ssh-remote+%s   # or VS Code: Connect to Host… → %s\n", path, alias, alias, alias)
			return nil
		},
	}
	c.Flags().StringVar(&alias, "alias", "", "SSH host alias (default: the workspace name)")
	c.Flags().StringVar(&user, "user", "", "remote user (default: your principal from `hopbox login`)")
	return c
}

// ensureInclude makes sure ~/.ssh/config pulls in the hopbox entries. OpenSSH
// requires Include before the first Host block, so we prepend it.
func ensureInclude(configPath string) error {
	const include = "Include hopbox/*.config"
	existing, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), include) {
		return nil
	}
	out := include + "\n\n" + string(existing)
	return os.WriteFile(configPath, []byte(out), 0o600)
}

// newSSHCmd is a convenience wrapper: it execs the system ssh with the right
// ProxyCommand inline, so `hopbox ssh <name> [cmd...]` works without writing any
// config first.
func newSSHCmd() *cobra.Command {
	var user string
	c := &cobra.Command{
		Use:                "ssh <name|id> [-- ssh args...]",
		Short:              "SSH into a workspace (wraps the system ssh)",
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			// crude flag handling so DisableFlagParsing still honours --user.
			user = ""
			var rest []string
			for i := 0; i < len(args); i++ {
				if args[i] == "--user" && i+1 < len(args) {
					user = args[i+1]
					i++
					continue
				}
				rest = append(rest, args[i])
			}
			if user == "" {
				if user = readPrincipal(); user == "" {
					user = "dev"
				}
			}
			if len(rest) == 0 {
				return fmt.Errorf("a workspace name is required")
			}
			name, extra := rest[0], rest[1:]
			self, err := os.Executable()
			if err != nil || self == "" {
				self = "hopbox"
			}
			idPath, _ := identityKeyPath()
			sshArgs := []string{
				"-o", fmt.Sprintf("ProxyCommand=%q proxy %s --addr %s", self, name, apiAddr),
				"-o", "StrictHostKeyChecking=accept-new",
			}
			if idPath != "" {
				sshArgs = append(sshArgs, "-o", "IdentityFile="+idPath, "-o", "IdentitiesOnly=yes")
			}
			sshArgs = append(sshArgs, user+"@"+name)
			sshArgs = append(sshArgs, extra...)
			ssh := exec.Command("ssh", sshArgs...)
			ssh.Stdin, ssh.Stdout, ssh.Stderr = os.Stdin, os.Stdout, os.Stderr
			return ssh.Run()
		},
	}
	return c
}
