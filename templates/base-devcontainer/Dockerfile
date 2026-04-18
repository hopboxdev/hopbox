FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    sudo curl wget git build-essential openssh-client \
    unzip xz-utils ca-certificates \
    netcat-openbsd socat ncurses-term \
    && rm -rf /var/lib/apt/lists/*

COPY ghostty.terminfo /tmp/ghostty.terminfo
RUN tic -x /tmp/ghostty.terminfo && rm /tmp/ghostty.terminfo

RUN existing=$(getent passwd 1000 | cut -d: -f1) && \
    if [ -n "$existing" ]; then userdel -r "$existing"; fi && \
    useradd -m -s /bin/bash -u 1000 dev && \
    echo "dev ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers.d/dev

COPY hop /usr/local/bin/hop
RUN chmod +x /usr/local/bin/hop

RUN printf '#!/bin/sh\nif [ -n "$BASH" ] && [ -f "$HOME/.bashrc" ]; then\n  . "$HOME/.bashrc"\nfi\n' > /etc/profile.d/hopbox-bashrc.sh \
    && chmod +x /etc/profile.d/hopbox-bashrc.sh

USER dev
WORKDIR /home/dev

CMD ["sleep", "infinity"]
