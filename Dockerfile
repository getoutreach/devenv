# syntax=docker/dockerfile:1.0-experimental
FROM gcr.io/outreach-docker/golang:1.17.1 AS builder
ARG VERSION
ENV GOCACHE "/go-build-cache"
ENV GOPRIVATE github.com/getoutreach/*
ENV CGO_ENABLED 0
WORKDIR /src


# Copy our source code into the container for building
COPY . .

# Cache dependencies across builds
RUN --mount=type=ssh --mount=type=cache,target=/go/pkg make dep

# Build our application, caching the go build cache, but also using
# the dependency cache from earlier.
RUN --mount=type=cache,target=/go/pkg --mount=type=cache,target=/go-build-cache \
  mkdir -p bin; \
  make BINDIR=/src/bin/ GO_EXTRA_FLAGS=-v


FROM gcr.io/outreach-docker/golang:1.17.1
ENTRYPOINT ["/usr/local/bin/devenv", "--skip-update"]

LABEL "io.outreach.reporting_team"="cia-dev-tooling"
LABEL "io.outreach.repo"="devenv"

###Block(afterBuild)
# Install runtime dependencies
RUN apk add --no-cache bash docker wget openssl sudo ncurses git openssh-client jq curl
RUN apk add --no-cache --repository=http://dl-cdn.alpinelinux.org/alpine/edge/testing kubectl
RUN apk add --no-cache --repository=http://dl-cdn.alpinelinux.org/alpine/edge/community vault libcap
RUN setcap cap_ipc_lock= /usr/sbin/vault
# Python
RUN apk add --no-cache python3 \
  &&  curl https://bootstrap.pypa.io/get-pip.py -o - | python3
RUN pip3 install yq
RUN wget -qO /tmp/kubecfg "https://github.com/bitnami/kubecfg/releases/download/v0.20.0/kubecfg-linux-amd64" \
  && chmod +x /tmp/kubecfg \
  && mv /tmp/kubecfg /usr/local/bin/
###EndBlock(afterBuild)

COPY --from=builder /src/bin/devenv /usr/local/bin/devenv
COPY --from=builder /src/bin/snapshot-uploader /usr/local/bin/snapshot-uploader
