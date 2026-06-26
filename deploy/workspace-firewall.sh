#!/bin/sh
# Egress firewall for hopbox workspace boxes.
#
# Front-door boxes are anonymous and run as root, so they should be treated as
# untrusted tenants of the host. Run hopboxd with `--compute-network hopbox-net`
# (puts boxes on a dedicated bridge with a fixed subnet) and apply this to fence
# their network: the agent hub and the internet are reachable; the host's other
# services, the LAN, the tailnet, and other docker networks are not.
#
#   sudo deploy/workspace-firewall.sh            # apply
#   sudo deploy/workspace-firewall.sh --remove   # revert
#
# Only traffic from the workspace subnet is touched, so a mistake can only affect
# the boxes (revert with --remove); the host and your other services are
# untouched. Re-run after a reboot, or wire it into a systemd unit.
#
# Overrides: HOPBOX_WS_SUBNET, HOPBOX_HUB (host IP the agent dials), HOPBOX_HUB_PORT.
set -eu

SUBNET="${HOPBOX_WS_SUBNET:-172.31.0.0/24}" # keep in sync with the provider's workspaceSubnet
HUB="${HOPBOX_HUB:-172.17.0.1}"             # host IP that hosts the agent hub
HUB_PORT="${HOPBOX_HUB_PORT:-7777}"

[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
command -v iptables >/dev/null 2>&1 || { echo "iptables required" >&2; exit 1; }

remove() {
	iptables -D INPUT -s "$SUBNET" -j HOPBOX-WS-IN 2>/dev/null || true
	iptables -D DOCKER-USER -s "$SUBNET" -j HOPBOX-WS-FWD 2>/dev/null || true
	iptables -F HOPBOX-WS-IN 2>/dev/null || true; iptables -X HOPBOX-WS-IN 2>/dev/null || true
	iptables -F HOPBOX-WS-FWD 2>/dev/null || true; iptables -X HOPBOX-WS-FWD 2>/dev/null || true
}

if [ "${1:-}" = "--remove" ]; then remove; echo "workspace firewall removed"; exit 0; fi

remove # idempotent: start from a clean slate

# INPUT — box -> host. Only the agent hub is reachable; every other host service
# (the API, gateway, your other containers' published ports, …) is denied.
iptables -N HOPBOX-WS-IN
iptables -A HOPBOX-WS-IN -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
iptables -A HOPBOX-WS-IN -d "$HUB" -p tcp --dport "$HUB_PORT" -j RETURN
iptables -A HOPBOX-WS-IN -j DROP
iptables -I INPUT -s "$SUBNET" -j HOPBOX-WS-IN

# FORWARD (via docker's DOCKER-USER hook) — box -> routed. The internet is
# allowed; private ranges, the tailnet, and link-local/metadata are denied.
iptables -N HOPBOX-WS-FWD
iptables -A HOPBOX-WS-FWD -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
iptables -A HOPBOX-WS-FWD -d 169.254.0.0/16 -j DROP
iptables -A HOPBOX-WS-FWD -d 10.0.0.0/8 -j DROP
iptables -A HOPBOX-WS-FWD -d 172.16.0.0/12 -j DROP
iptables -A HOPBOX-WS-FWD -d 192.168.0.0/16 -j DROP
iptables -A HOPBOX-WS-FWD -d 100.64.0.0/10 -j DROP
iptables -A HOPBOX-WS-FWD -j RETURN # everything else = public internet
iptables -I DOCKER-USER -s "$SUBNET" -j HOPBOX-WS-FWD

echo "workspace firewall applied — subnet=$SUBNET hub=$HUB:$HUB_PORT (internet egress kept)"
