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

## SSH front door

A krillbox-style entry point: `ssh <spec>@host` where the **username is a
workspace spec** and the **client key is the identity** — no signup, no
pre-created workspace. Boxes spawned this way are ephemeral. See
[SSH & VS Code → Front door](/guide/ssh#ephemeral-front-door).

| Flag | Default | Description |
| --- | --- | --- |
| `--ssh-addr` | _(empty)_ | Front-door SSH listen address (e.g. `:2222`); empty disables. |
| `--ssh-host-key` | `./hopbox-ssh-front-key` | Front-door host key path (auto-created on first run). |
| `--ssh-default-image` | `alpine` | Image for front-door boxes when the username names none. |
| `--ssh-default-mem-mb` | `2048` | Memory cap (MB) for front-door boxes — they are anonymous, so this bounds how much a single box can consume. `0` = unlimited. |

::: warning The front door is anonymous
With the default `AnyKey` authority, **any** client key spawns a box (the key is
the identity). Restrict reachability of `--ssh-addr` to a trusted network, and
keep the memory cap in place so a box cannot exhaust the host. Front-door boxes
run as root in the container with normal network egress — treat them as untrusted
tenants of the host.
:::

## Reconcile wake-ups (events bus)

The reconciler runs a hybrid loop: an event wake-up reconciles a single
workspace the instant its state changes (e.g. an SSH session detaches), while an
interval sweep is the backstop that catches anything missed. The bus carrying
those wake-ups is pluggable.

| Flag | Default | Description |
| --- | --- | --- |
| `--events` | `inproc` | Wake-up bus: `inproc` (in-process, zero deps) \| `nats` (fans wake-ups across nodes). |
| `--nats-url` | `nats://127.0.0.1:4222` | NATS server URL when `--events=nats`. |

`inproc` keeps a single-node deployment dependency-free. Use `nats` when the
agent hub and the reconciler run on different hosts.
