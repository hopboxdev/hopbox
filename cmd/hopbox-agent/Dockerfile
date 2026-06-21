# Minimal agent image: the static linux/amd64 hopbox-agent at a known path.
# AgentImage.ImageRef points here; BinaryPath is /hopbox-agent. Reused by the
# docker copy-into-volume path and the kubernetes initContainer path.
# Build the binary first: `make agent` (produces bin/hopbox-agent-linux-amd64).
FROM scratch
COPY bin/hopbox-agent-linux-amd64 /hopbox-agent
ENTRYPOINT ["/hopbox-agent"]
