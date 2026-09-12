# syntax=docker/dockerfile:1
# SPDX-License-Identifier: AGPL-3.0-or-later

# Three stages, so that neither Node nor Go is present in what ships. The two builder stages are
# pinned to $BUILDPLATFORM and cross-compile to $TARGETARCH: emulating an arm64 or armv7 build
# under QEMU would take many times longer for an identical, pure-Go result.

# ---- Stage 1: the dashboard -------------------------------------------------------------------
# Node exists only here. The compiled bundle is embedded into the binary by web/embed.go, so the
# runtime image needs no JavaScript toolchain and serves no files from disk.
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS web
WORKDIR /src
RUN corepack enable
# package.json's packageManager field pins pnpm; copying the manifests before the sources keeps
# the dependency layer cached across source-only changes.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
COPY web/package.json web/package.json
RUN pnpm install --frozen-lockfile
COPY web web
COPY schemas schemas
RUN pnpm --filter veduta-web build

# ---- Stage 2: the binary ----------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The SPA must be in place before `go build`: //go:embed all:build resolves at compile time, and a
# stale or missing build/app would silently produce a binary that serves no dashboard.
COPY --from=web /src/web/build web/build

ARG TARGETARCH
ARG TARGETVARIANT
# Supplied by the release workflow from the tag; the defaults keep a local `docker build` honest
# rather than making it claim to be a release.
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ENV CGO_ENABLED=0 GOOS=linux
# TARGETVARIANT is "v7" for linux/arm/v7; GOARM wants the bare number.
RUN GOARCH="$TARGETARCH" GOARM="${TARGETVARIANT#v}" go build -trimpath \
      -ldflags "-s -w \
        -X veduta.dev/veduta/internal/version.Version=${VERSION} \
        -X veduta.dev/veduta/internal/version.Commit=${COMMIT}" \
      -o /out/veduta ./cmd/veduta

# The same staging script the release archives use, so the image and the tarball can never ship
# different sets. It also re-checks each module against the digest its manifest pins.
RUN hack/stage-plugins.sh /out/plugins
# An empty directory to copy into the final stage: distroless has no shell, so the only way to
# give /data the right ownership is to carry a directory that already has it. Without this the
# runtime image has no /data at all, and Docker seeds a named volume mounted there as root:root -
# which uid 65532 cannot write, and SQLite reports as "unable to open database file".
RUN mkdir -p /out/data

# ---- Stage 3: what ships ----------------------------------------------------------------------
# distroless/static has no shell and no package manager: the binary is the only executable in the
# image. It supplies the CA bundle Veduta needs to reach HTTPS services and the zoneinfo database
# rules and schedules format timestamps against.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/veduta /usr/local/bin/veduta
# The first-party integrations, so a configuration can reference them without mounting anything.
# Read-only by construction; third-party plugins are mounted by the operator and approved in
# veduta.lock.yaml exactly as they are outside a container.
COPY --from=build /out/plugins /usr/share/veduta/plugins

# /config holds veduta.yaml, conf.d/ and veduta.lock.yaml; /data holds the SQLite database and the
# asset and icon caches. Both are mounted by the operator. /data exists in the image, owned by the
# runtime uid, so that a named volume mounted over it inherits that ownership instead of root's.
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME ["/data"]
EXPOSE 8099
# 65532:65532, from the :nonroot tag. Veduta needs no privileges, and /data must be writable by
# this uid - see docs/docker.md.
USER nonroot:nonroot

# The binary probes itself: there is no shell or curl in this image to do it instead.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/veduta", "health", "--addr", "http://127.0.0.1:8099"]

ENTRYPOINT ["/usr/local/bin/veduta"]
# server.listen must be 0.0.0.0:8099 in the mounted config for the port to be reachable, and
# Veduta refuses a non-loopback bind unless authentication is configured. That refusal is the
# intended behaviour in a container too - see docs/docker.md.
CMD ["serve", "--config", "/config/veduta.yaml", "--data-dir", "/data"]
