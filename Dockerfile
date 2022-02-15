# syntax=docker/dockerfile:1.0-experimental
FROM gcr.io/outreach-docker/golang:1.17.5 AS builder
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


FROM alpine:3.15
ENTRYPOINT ["/usr/local/bin/devenv", "--skip-update"]

LABEL "io.outreach.reporting_team"="fnd-dtss"
LABEL "io.outreach.repo"="devenv"

###Block(afterBuild)
###EndBlock(afterBuild)

COPY --from=builder /src/bin/devenv /usr/local/bin/devenv
COPY --from=builder /src/bin/snapshot-uploader /usr/local/bin/snapshot-uploader
