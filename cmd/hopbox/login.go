package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"

	hopboxv1 "github.com/hopboxdev/hopbox/gen/hopbox/v1"
)

// hopboxDir is ~/.hopbox, holding the user's SSH identity + issued certificate.
func hopboxDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".hopbox")
	return d, os.MkdirAll(d, 0o700)
}

func identityKeyPath() (string, error) {
	d, err := hopboxDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "id_ed25519"), nil
}

// readPrincipal returns the principal recorded by the last `hopbox login`.
func readPrincipal() string {
	d, err := hopboxDir()
	if err != nil {
		return ""
	}
	b, _ := os.ReadFile(filepath.Join(d, "principal"))
	return strings.TrimSpace(string(b))
}

// readToken returns the saved api token (from `hopbox login --token`), if any.
func readToken() string {
	d, err := hopboxDir()
	if err != nil {
		return ""
	}
	b, _ := os.ReadFile(filepath.Join(d, "token"))
	return strings.TrimSpace(string(b))
}

// newLoginCmd ensures a local SSH key exists and exchanges it for a short-lived
// certificate signed by the server's CA — the credential `ssh`/VS Code present.
func newLoginCmd(dial func() (hopboxv1.WorkspaceServiceClient, func(), error)) *cobra.Command {
	var token string
	c := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and fetch a short-lived SSH certificate",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// --token saves the api token first, so the cert request below (and all
			// later calls) authenticate as this user on multi-user servers.
			if token != "" {
				d, err := hopboxDir()
				if err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(d, "token"), []byte(token), 0o600); err != nil {
					return err
				}
			}
			keyPath, err := identityKeyPath()
			if err != nil {
				return err
			}
			if _, err := os.Stat(keyPath); os.IsNotExist(err) {
				if err := generateIdentity(keyPath); err != nil {
					return err
				}
				fmt.Printf("generated SSH key %s\n", keyPath)
			}
			pubLine, err := os.ReadFile(keyPath + ".pub")
			if err != nil {
				return fmt.Errorf("read public key: %w", err)
			}

			client, closer, err := dial()
			if err != nil {
				return err
			}
			defer closer()
			resp, err := client.IssueSSHCert(context.Background(), &hopboxv1.IssueSSHCertRequest{
				PublicKey: string(pubLine),
			})
			if err != nil {
				return err
			}

			if err := os.WriteFile(keyPath+"-cert.pub", []byte(resp.Certificate), 0o644); err != nil {
				return err
			}
			d, _ := hopboxDir()
			_ = os.WriteFile(filepath.Join(d, "principal"), []byte(resp.Principal), 0o600)

			fmt.Printf("logged in as %q — certificate valid until %s\n",
				resp.Principal, time.Unix(resp.ValidBeforeUnix, 0).Format(time.RFC1123))
			fmt.Printf("next: hopbox ssh-config <workspace>   then   ssh <workspace>   (or: hopbox ssh <workspace>)\n")
			return nil
		},
	}
	c.Flags().StringVar(&token, "token", "", "api token for multi-user servers (saved to ~/.hopbox/token)")
	return c
}

// generateIdentity writes a new ed25519 SSH keypair at path (+ ".pub").
func generateIdentity(path string) error {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return err
	}
	signer, err := ssh.NewSignerFromSigner(priv)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(signer.PublicKey()), 0o644)
}
