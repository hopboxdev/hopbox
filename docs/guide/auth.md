# Auth & multi-user

Hopbox scales from a solo self-hoster to an org of many users on one control
plane, where **each user gets their own boxes and their own SSH keys** — isolated
from everyone else's. The same machinery covers all three; you pick how callers
authenticate.

| Mode | Identity | SSH credential |
| --- | --- | --- |
| **Open** (default) | none — single implicit user | built-in CA (`hopbox login`) |
| **Team** | static token file (`--users`) | built-in CA, per-user certs |
| **Org** | OIDC / SSO (`--oidc-issuer`) | built-in CA, or trust an **external CA** |

## How access works

Hopbox doesn't push a list of public keys to every box. Instead each box **trusts
one CA public key**, and users present a **short-lived certificate** the CA signed
for their principal. Add or remove people centrally; nothing to distribute to
boxes; access expires on its own.

When you run `hopbox login`, the CLI gets a certificate naming **your** principal.
A box admits a certificate only if it's signed by the trusted CA **and** names the
box's owner — so every box can trust the same CA, yet only its owner's cert opens
it.

## Open mode (solo)

The default. No tokens, no logins beyond `hopbox login` for a cert. One implicit
user owns everything. Nothing to configure.

## Team mode — static tokens

Give each teammate a token mapped to a principal. Create a users file:

```
# /etc/hopbox/users  —  <token>  <principal>
s3cret-alice   alice
s3cret-bob     bob
```

Start the server with it:

```sh
hopboxd --users /etc/hopbox/users
```

Each user authenticates once; the token is sent on every call and saved locally:

```sh
hopbox login --token s3cret-alice
hopbox create mybox --image ubuntu:24.04
hopbox ssh mybox
```

Now `alice` and `bob` each see and reach only their own workspaces — `hopbox ls`,
`ssh`, `exec`, and `rm` are all scoped to the caller; another user's box returns
`not found`.

## Org mode — OIDC / SSO

Point Hopbox at your identity provider (Google, Okta, Entra, Keycloak, …). Users
authenticate with an OIDC ID token instead of a hand-managed token, and identity,
group membership, and revocation stay in the IdP.

```sh
hopboxd \
  --oidc-issuer https://accounts.google.com \
  --oidc-audience <client-id> \
  --oidc-principal-claim email \
  --oidc-admin-groups platform-admins
```

- `--oidc-principal-claim` chooses whether the user id is the token's `sub`
  (default) or `email`.
- Membership in `--oidc-admin-groups` grants the `tenant-admin` role (sees all
  workspaces).

::: tip CLI token
Today the CLI sends the OIDC token like any other: `hopbox login --token <jwt>`.
A browser device-flow (`hopbox login --oidc`) that fetches and refreshes the
token automatically is on the roadmap.
:::

## Bring your own SSH CA

Enterprises often already run an SSH CA (HashiCorp Vault SSH, Smallstep, Teleport)
where issuance, audit, and revocation live. Point Hopbox at that CA's **public
key** instead of using the built-in one:

```sh
hopboxd --ssh-ca-pub /etc/hopbox/org-ca.pub
```

Workspaces now trust your CA, and Hopbox's own `hopbox login` issuance is disabled
— your existing tooling mints the certs (which must name the workspace owner as a
principal). This composes with OIDC: the OIDC identity owns the box, your CA
issues a cert for that identity, and the box admits only the owner.

## See also

- [SSH & VS Code](/guide/ssh) — the client side: ssh-config, VS Code, scp/rsync.
- [`hopboxd` config](/reference/hopboxd) — every auth and CA flag.
