# Minimal agent image: the static linux/amd64 mesa-agent at a known path.
# AgentImage.ImageRef points here; BinaryPath is /mesa-agent. Reused by the
# docker copy-into-volume path and the kubernetes initContainer path.
# Build the binary first: `make agent` (produces bin/mesa-agent-linux-amd64).
FROM scratch
COPY bin/mesa-agent-linux-amd64 /mesa-agent
ENTRYPOINT ["/mesa-agent"]
