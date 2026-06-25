---
layout: home
hero:
  name: Hopbox
  text: Your dev environments, anywhere.
  tagline: A self-hosted, vendor-neutral control plane for development environments. Reproducible workspaces on Docker or Kubernetes, reachable anywhere via a reverse-dialing agent — or spun up on demand with a single SSH command.
  image:
    src: /logo.svg
    alt: Hopbox
  actions:
    - theme: brand
      text: Quickstart
      link: /guide/quickstart
    - theme: alt
      text: Install the CLI
      link: /guide/quickstart#install-the-cli
    - theme: alt
      text: What is Hopbox
      link: /guide/what-is-hopbox
    - theme: alt
      text: GitHub
      link: https://github.com/hopboxdev/hopbox
features:
  - title: A box in one SSH command
    details: '`ssh proj@host` and you are in a fresh workspace — the username is the spec, your key is the identity. No signup, no pre-provisioning.'
  - title: Reachable anywhere
    details: Workspaces dial out to the control plane; nothing routes in. Reach them even behind NAT or inside a private cluster.
  - title: Ephemeral or persistent
    details: Front-door boxes are temporary — reaped when you disconnect, with an optional grace window. Or create workspaces that stick around.
  - title: Native SSH & VS Code
    details: '`ssh mybox`, VS Code "Connect to Host", scp and rsync — all through short-lived SSH certificates, with no public port exposed.'
  - title: Multi-user by design
    details: Every user gets their own boxes and their own keys. Static tokens for a team, OIDC SSO for an org.
  - title: Swappable providers
    details: Compute, storage, ingress, identity, build, metering — six versioned contracts. Docker, Kubernetes, or your own stack.
---
