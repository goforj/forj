FROM debian:trixie@sha256:f324c7ff54321e8d9c588493a20244965938ce0aa50bbd1022d38010e9ffc4b1

# Versions (override at build time if desired)
ARG GO_VERSION=1.26.6
ARG GO_AMD64_SHA256=708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89
ARG GO_ARM64_SHA256=d0507e9e9d7fe012aae570108cbd76c15de879e17130ab8cb90d4d7445cb1f2e

# Apply security fixes to inherited packages before adding Docker Engine dependencies.
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gnupg \
    lsb-release \
    iptables \
    uidmap \
    fuse-overlayfs \
    && rm -rf /var/lib/apt/lists/*

# -------------------------------------------------------------
# Install Docker Engine (client + dockerd)
# -------------------------------------------------------------
RUN install -m 0755 -d /etc/apt/keyrings && \
    curl -fsSL https://download.docker.com/linux/debian/gpg \
      -o /etc/apt/keyrings/docker.gpg && \
    chmod a+r /etc/apt/keyrings/docker.gpg && \
    echo \
      "deb [arch=$(dpkg --print-architecture) \
      signed-by=/etc/apt/keyrings/docker.gpg] \
      https://download.docker.com/linux/debian \
      $(. /etc/os-release; echo $VERSION_CODENAME) stable" \
      > /etc/apt/sources.list.d/docker.list

RUN apt-get update && apt-get install -y --no-install-recommends \
    docker-ce \
    docker-ce-cli \
    containerd.io \
    docker-buildx-plugin \
    docker-compose-plugin \
    && rm -rf /var/lib/apt/lists/*

RUN ln -s /usr/libexec/docker/cli-plugins/docker-compose /usr/local/bin/docker-compose

# -------------------------------------------------------------
# Install Go
# -------------------------------------------------------------
RUN arch="$(dpkg --print-architecture)" && \
    case "$arch" in \
      amd64) checksum="$GO_AMD64_SHA256" ;; \
      arm64) checksum="$GO_ARM64_SHA256" ;; \
      *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac && \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz" -o /tmp/go.tgz && \
    echo "${checksum}  /tmp/go.tgz" | sha256sum -c - && \
    tar -C /usr/local -xzf /tmp/go.tgz && \
    rm /tmp/go.tgz

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV PATH="${GOPATH}/bin:/usr/local/go/bin:${PATH}"

RUN mkdir -p /go && chmod -R 777 /go

# -------------------------------------------------------------
# Docker-in-Docker setup
# -------------------------------------------------------------
# Default working directory for Docker
RUN mkdir -p /var/lib/docker

# Standard DinD entrypoint (similar to docker:dind)
COPY --from=docker:27-dind@sha256:aa3df78ecf320f5fafdce71c659f1629e96e9de0968305fe1de670e0ca9176ce /usr/local/bin/dind /usr/local/bin/dind
COPY --from=docker:27-dind@sha256:aa3df78ecf320f5fafdce71c659f1629e96e9de0968305fe1de670e0ca9176ce /usr/local/bin/docker-init /usr/local/bin/docker-init

# Make dockerd the default entrypoint
ENTRYPOINT ["/usr/local/bin/dind"]

CMD ["dockerd"]
