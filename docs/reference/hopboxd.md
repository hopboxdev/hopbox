# `hopboxd` configuration

`hopboxd` is the control plane. All settings are flags (env/file config is on the
roadmap). Run `hopboxd --help` for the authoritative list.

## Core

| Flag | Default | Description |
| --- | --- | --- |
| `--api-addr` | `:7700` | gRPC API listen address (CLI clients). |
| `--agent-listen` | `:7777` | Address agents dial in on. |
| `--agent-advertise` | `host.docker.internal:7777` | Address agents are told to dial back (must be reachable from inside a workspace). |
| `--db` | `./hopbox.db` | SQLite database path. |
| `--tenant` | `default` | Single-tenant id. |

## Compute & storage

| Flag | Default | Description |
| --- | --- | --- |
| `--compute` | `docker` | Compute provider: `docker` \| `kubernetes`. |
| `--storage` | `localfs` | Storage provider: `localfs` \| `k8spvc`. |
| `--agent-bin` | `./bin/hopbox-agent-linux-<arch>` | Host path of the agent binary side-loaded into workspaces. |

Kubernetes options: `--kube-namespace`, `--kubeconfig`, `--kube-storageclass`,
`--kube-home-size`.

## Auth (multi-user)

| Flag | Default | Description |
| --- | --- | --- |
| `--users` | _(empty)_ | Token→principal file (`<token> <principal>` per line). Enables multi-user auth. Empty = open single-user mode. |
| `--oidc-issuer` | _(empty)_ | OIDC issuer URL for SSO auth. Overrides `--users`. |
| `--oidc-audience` | _(empty)_ | Expected token audience (client id). |
| `--oidc-principal-claim` | `sub` | Claim used as the principal id: `sub` \| `email`. |
| `--oidc-admin-groups` | _(empty)_ | Comma-separated groups granted the `tenant-admin` role. |

See [Auth & multi-user](/guide/auth).

## SSH certificates

| Flag | Default | Description |
| --- | --- | --- |
| `--ssh-ca` | `./hopbox-ssh-ca` | Built-in SSH user-CA private key (auto-created). Workspaces trust its public key; `hopbox login` issues certs from it. |
| `--ssh-ca-pub` | _(empty)_ | Trust an **external** SSH CA public key instead. Disables built-in issuance — your own tooling mints certs. |
| `--authorized-keys` | _(empty)_ | Fallback static `authorized_keys` file injected into workspaces (no-login mode). |

## Gateway (HTTPS ingress)

| Flag | Default | Description |
| --- | --- | --- |
| `--gateway-addr` | `:8088` | Service gateway HTTP listen address; empty disables. |
| `--gateway-zone` | `gw.example.com` | Wildcard DNS zone for the subdomain ingress provider. |
| `--tunnel-addr` | `:7701` | Gateway tunnel listen address for a standalone `hopbox-gw`; empty disables. |
