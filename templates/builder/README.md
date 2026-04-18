# hopbox builder image

Short-lived OCI image that hopboxd runs to build per-user devcontainer images. Contains `node`, `@devcontainers/cli`, and the Docker CLI.

Published as `ghcr.io/hopboxdev/builder:<version>`.

## Build locally

    make builder-image

## Invocation shape (hopboxd does this automatically)

    docker run --rm \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -v <workspace>:/workspace:ro \
      ghcr.io/hopboxdev/builder:<version> \
      build --workspace-folder /workspace --image-name <tag>
