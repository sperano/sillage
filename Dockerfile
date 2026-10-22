# syntax=docker/dockerfile:1

# The sillage image: cmd/sillage with the built frontend embedded (-tags
# webdist), for linux/amd64 and linux/arm64. The web and go stages run on
# the build platform and Go cross-compiles for the target, so the frontend is
# built once and neither Node nor Go runs under emulation.
#
#   docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=<version> .

# Node satisfying web/package.json's engines, the major CI uses.
FROM --platform=$BUILDPLATFORM node:22.23-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc ./
# No dependency needs an install script (the browser for the tests is
# downloaded by an explicit command, never at install), so none runs.
RUN --mount=type=cache,target=/root/.npm npm ci --ignore-scripts
COPY web/ ./
# Writes dist/, and dist/.vite/license.md for the bundled dependencies.
RUN npm run build

# The module's go line.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS go
ARG TARGETARCH
# The release this image is built from, logged at startup; the binary
# reports "dev" when it is unset or empty.
ARG VERSION
ENV CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH
WORKDIR /src
# go.sum* lets this line stand before the module has a go.sum: a wildcard
# matching nothing is not an error while another source (go.mod) exists.
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# No fallback: without web/dist, the webdist build fails to compile.
COPY --from=web /src/web/dist web/dist
RUN --mount=type=cache,target=/root/.cache/go-build \
	go build -tags webdist -trimpath \
		-ldflags "-s -w ${VERSION:+-X main.version=$VERSION}" \
		-o /out/sillage ./cmd/sillage
# The image distributes the binary, so it carries the licenses of what it
# links.
RUN GOFLAGS=-tags=webdist sh scripts/collect-licenses.sh /out/licenses

# scratch: a static binary needs no libc, shell or user database.
FROM scratch
# Every source is reached over HTTPS.
COPY --from=go /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=go /src/LICENSE /licenses/sillage/LICENSE
COPY --from=go /out/licenses /licenses
COPY --from=web /src/web/dist/.vite/license.md /licenses/web/license.md
COPY --from=go /out/sillage /sillage
# Numeric, so that Kubernetes' runAsNonRoot can check it without a passwd
# file. The binary writes nothing to disk: the root filesystem can be
# read-only.
USER 65532:65532
# cmd/sillage's defaultListenAddr; SILLAGE_LISTEN_ADDR changes it.
EXPOSE 8080
# No HEALTHCHECK: scratch has no shell or curl; probe GET /healthz instead.
# Exec form, so SIGTERM reaches the binary, which shuts down gracefully.
ENTRYPOINT ["/sillage"]
