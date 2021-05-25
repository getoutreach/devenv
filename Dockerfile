# syntax=docker/dockerfile:1.0-experimental
FROM gcr.io/outreach-docker/golang:1.16.2 AS builder
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


FROM gcr.io/outreach-docker/alpine:3.13
ENTRYPOINT ["/usr/local/bin/devenv", "--skip-update"]

LABEL "io.outreach.reporting_team"="cia-dev-tooling"
LABEL "io.outreach.repo"="devenv"

# Add timezone information.
COPY --from=builder /usr/local/go/lib/time/zoneinfo.zip /zoneinfo.zip
ENV ZONEINFO=/zoneinfo.zip

###Block(afterBuild)
# Install runtime dependencies
RUN apk add --no-cache bash docker wget openssl sudo ncurses git openssh-client jq curl
RUN apk add --no-cache --repository=http://dl-cdn.alpinelinux.org/alpine/edge/testing kubectl
# Python
RUN apk add --no-cache python3 \
  &&  curl https://bootstrap.pypa.io/get-pip.py -o - | python3
RUN pip3 install yq
RUN wget -qO /tmp/kubecfg "https://github.com/bitnami/kubecfg/releases/download/v0.20.0/kubecfg-linux-amd64" \
  && chmod +x /tmp/kubecfg \
  && mv /tmp/kubecfg /usr/local/bin/
###EndBlock(afterBuild)

COPY --from=builder /src/bin/devenv /usr/local/bin/devenv
