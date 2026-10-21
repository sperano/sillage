# Sillage

A read-only history of what people, agents and automation did on a task,
across Vikunja, Kelos, GitHub and Wiki.js, on one timeline. *Sillage* is
French for the wake a boat leaves behind.

Sillage only observes: it is never in the path that starts agent work, so it
being down never blocks anything.

## Status

Pre-alpha. This is a skeleton: a Go binary that answers `GET /healthz` and
serves a placeholder page. No source is read, nothing is stored, nothing is
deployed.

## Layout

| Path | Contents |
| --- | --- |
| `cmd/sillage/` | The binary (collector, read API and embedded frontend, eventually). |
| `internal/model/` | Normalized events, runs, entities and coverage. |
| `internal/source/{vikunja,github,kelos,wiki}/` | One read-only source each. |
| `internal/store/` | Persistence. |
| `internal/api/` | Read-only HTTP API. |
| `testdata/fixtures/` | Fabricated test data, shared by Go and web tests. |
| `web/` | Svelte 5 + Vite single-page frontend (TypeScript), embedded into the binary. |
| `web/placeholder/` | Page embedded when the frontend is not built. |

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
