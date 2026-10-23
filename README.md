# Sillage

A read-only history of what people, agents and automation did on a task,
across Vikunja, Kelos, GitHub and Wiki.js, on one timeline. *Sillage* is
French for the wake a boat leaves behind.

Sillage only observes: it is never in the path that starts agent work, so it
being down never blocks anything.

## Status

Pre-alpha. A Go binary that answers `GET /healthz` and serves a placeholder
page, and the normalized model (`internal/model`) with fabricated fixture
stories (`testdata/fixtures/stories/`), and a PostgreSQL store
(`internal/store`) that nothing writes to yet. Nothing is deployed.

## Layout

| Path | Contents |
| --- | --- |
| `cmd/sillage/` | The binary (collector, read API and embedded frontend, eventually). |
| `internal/model/` | Normalized events, runs, entities and coverage. |
| `internal/source/{vikunja,github,kelos,wiki}/` | One read-only source each. |
| `internal/store/` | PostgreSQL persistence, with its migrations embedded. |
| `internal/api/` | Read-only HTTP API. |
| `testdata/fixtures/` | Fabricated test data, shared by Go and web tests. |
| `web/` | Svelte 5 + Vite single-page frontend (TypeScript), embedded into the binary. |
| `web/placeholder/` | Page embedded when the frontend is not built. |
| `Dockerfile` | The image: the binary with the frontend embedded, `linux/amd64` and `linux/arm64`. |

Deployment lives in [`sperano/hollingsworth`](https://github.com/sperano/hollingsworth).
[`AGENTS.md`](AGENTS.md) has the conventions and rules for contributors.

## Build and run

Requires Go 1.27. Node 22.12+ only for the frontend.

```sh
go build ./cmd/sillage                     # embeds web/placeholder/
./sillage -listen :8080                    # or SILLAGE_LISTEN_ADDR=:8080; default :8080
curl localhost:8080/healthz                # ok

(cd web && npm ci && npm run build)        # builds web/dist/
go build -tags webdist ./cmd/sillage       # embeds web/dist/
```

With `SILLAGE_DATABASE_URL` (a PostgreSQL connection string, never
logged) set, the binary applies the store's embedded migrations at startup
and prunes by `SILLAGE_RETENTION` (`90d`, `720h`, or the default
`forever`; at least a day). Without it, nothing is stored.

The image builds both (no local `web/dist/` needed) and embeds the
frontend:

```sh
docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=<version> -t sillage:dev .
docker buildx build --load --build-arg VERSION=<version> -t sillage:dev .   # this machine's platform only
docker run --rm --read-only -p 8080:8080 sillage:dev
curl localhost:8080/healthz                # ok
```

It runs as a non-root user on `scratch`, logs its version at startup (`dev`
without `VERSION`), and carries the licenses of everything it ships under
`/licenses`.

## Test

The same commands as CI (`.github/workflows/ci.yml`).

Go, from the repository root:

```sh
test -z "$(gofmt -l $(git ls-files -co --exclude-standard '*.go'))"
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@2026.2.2 ./...
go test -race ./...
go build ./...
```

The store's tests need a PostgreSQL they may create schemas in, named by
`SILLAGE_TEST_DATABASE_URL`; without it they skip:

```sh
SILLAGE_TEST_DATABASE_URL=postgres://sillage@localhost:5432/sillage_test?sslmode=disable go test -race ./internal/store
```

Web, from `web/`:

```sh
npm ci
npx playwright install --with-deps chromium   # once: Chromium for the browser tests
npm run check
npm run lint
npm test
npm run build
```

Then, from the repository root, with the frontend built:

```sh
go vet -tags=webdist ./... && go test -tags=webdist ./...
```

## License

GNU General Public License v3.0; see [LICENSE](LICENSE).
