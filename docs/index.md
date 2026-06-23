---
layout: home
hero:
  name: Hopbox
  text: Your dev environments, anywhere.
  tagline: A self-hosted, vendor-neutral control plane for development environments — reproducible workspaces on Docker or Kubernetes, reachable anywhere via a reverse-dialing agent.
  image:
    src: /logo.svg
    alt: Hopbox
  actions:
    - theme: brand
      text: Quickstart
      link: /guide/quickstart
    - theme: alt
      text: What is Hopbox
      link: /guide/what-is-hopbox
    - theme: alt
      text: GitHub
      link: https://github.com/hopboxdev/hopbox
features:
  - title: Reachable anywhere
    details: Workspaces dial out to the control plane; nothing routes in. Reach them even behind NAT or inside a private cluster.
  - title: Native SSH & VS Code
    details: '`ssh mybox`, VS Code "Connect to Host", scp and rsync — all through short-lived SSH certificates, with no public port exposed.'
  - title: Multi-user by design
    details: Every user gets their own boxes and their own keys. Static tokens for a team, OIDC SSO for an org.
  - title: Swappable providers
    details: Compute, storage, ingress, identity, build, metering — six versioned contracts. Docker, Kubernetes, or your own stack.
---
