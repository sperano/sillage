# Sillage

A read-only history of what people, agents and automation did on a task,
across Vikunja, Kelos, GitHub and Wiki.js, on one timeline. *Sillage* is
French for the wake a boat leaves behind.

Sillage only observes: it is never in the path that starts agent work, so it
being down never blocks anything.

## Status

Pre-alpha. A Go binary that answers `GET /healthz`, serves a placeholder
page and the read-only JSON API under `/api/v1/`; the normalized model
(`internal/model`) with fabricated fixture stories
(`testdata/fixtures/stories/`); a read-only client per source; `sillage
story`, which assembles one story from the live sources and prints it; and
a PostgreSQL store (`internal/store`) that nothing writes to yet. The
frontend's single-task timeline graph (`web/src/lib/timeline/`) is
rendered in tests from the fixture stories but not yet shown on the story
page. Nothing is deployed.

## Layout

| Path | Contents |
| --- | --- |
| `cmd/sillage/` | The binary: health check, read API and embedded frontend (the collector, eventually). |
| `internal/model/` | Normalized events, runs, entities and coverage. |
| `internal/source/{vikunja,github,kelos,wiki}/` | One read-only source each. |
| `internal/collector/` | Joins the sources' records into one story. |
| `internal/store/` | PostgreSQL persistence, with its migrations embedded. |
| `internal/api/` | Read-only JSON API over the store. |
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
`forever`; at least a day). Without it, nothing is stored, and the API
answers 503 for everything but its version.

The API (`internal/api`, all `GET`, JSON, `Cache-Control: no-store`, errors
as RFC 9457 problem bodies) has no authentication of its own: `/api/` must
stay behind the Authentik forward-auth in front of sillage.

| Route | Answers |
| --- | --- |
| `/api/v1/stories/task/{instance}/{id}`, `/api/v1/stories/page/{instance}/{id}/{locale}` | The stored `model.Story`, as the golden files under `testdata/fixtures/stories/` |
| `/api/v1/coverage/task/{instance}/{id}`, `/api/v1/coverage/page/{instance}/{id}/{locale}` | The root's coverage: last successful read and gaps per source |
| `/api/v1/coverage` | Per source instance, the newest and stalest last read across stored roots |
| `/api/v1/search?q=&limit=` | Roots by title or key, most recently active first (`q` at most 200 characters) |
| `/api/v1/resolve?url=` | The roots whose stories hold what a Vikunja task, GitHub pull request or wiki page URL, or a Kelos Task (`[<namespace>/]<name>`, UID), points to; 404 if none |
| `/api/v1/version` | The binary's version |

A wiki URL by path (`/<locale>/<path>`) resolves only with the wiki source
configured (`SILLAGE_WIKI_URL`, `SILLAGE_WIKI_TOKEN`,
`SILLAGE_WIKI_AGENT_USERS`), whose page listing maps it to a page id; by id
(`/i/<id>`) it always does. Every other `GET` path that is no frontend file
serves the frontend's `index.html`, so that deep links to its routes work,
unless its last segment has an extension (`/app.js`): a missing file is a
404.

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

## Print one story

`sillage story` reads one task's or wiki page's records from the live
sources, joins them, and prints the story as JSON: the format of
`testdata/fixtures/stories/*/story.golden.json`. It writes nothing anywhere.

```sh
./sillage story -task <id>                 # a Vikunja task
./sillage story -page <id> [-locale en]    # a wiki page, for work requested there
./sillage story -task <id> -repo owner/name   # also read the task's branch there
```

A source that cannot be read is reported on stderr and leaves a coverage
record without a read; the story is still printed, and the exit status is 3.
The flags name the root and any extra repository; every source setting
comes from the environment, and each source reads with a read-only
credential:

| Variable | Meaning |
| --- | --- |
| `SILLAGE_VIKUNJA_URL`, `SILLAGE_VIKUNJA_TOKEN` | Vikunja base URL and a read-only API token. |
| `SILLAGE_VIKUNJA_SERVICE_ACCOUNT` | Username that posts Kelos start announcements (default `kelos`). |
| `SILLAGE_VIKUNJA_AGENT_USERS` | Agents' Vikunja usernames, comma-separated: their comments are an agent's, and only theirs and the service account's are followed to the wiki pages they link (a discussion's deliverable). |
| `SILLAGE_KELOS_CLUSTER`, `SILLAGE_KELOS_NAMESPACE` | Name given to the cluster, and the namespace read; the Kubernetes credentials are the in-cluster ones, else the kubeconfig's current context. |
| `SILLAGE_KELOS_LABEL_DOMAIN`, `SILLAGE_KELOS_VIKUNJA_INSTANCE` | The spawners' label prefix, and the Vikunja host the task label names (must be `SILLAGE_VIKUNJA_URL`'s). |
| `SILLAGE_KELOS_TASK_TTL` | How long Kelos keeps a finished Task (default `72h`): runs that ended longer ago than that may be missing. |
| `SILLAGE_GITHUB_TOKEN`, `SILLAGE_GITHUB_API_URL` | A read-only token, and the API (default `https://api.github.com`). |
| `SILLAGE_WIKI_URL`, `SILLAGE_WIKI_TOKEN`, `SILLAGE_WIKI_AGENT_USERS` | Wiki.js base URL, a read-only key, and the agents' wiki user ids. |
| `SILLAGE_WORKSPACES` | `<workspace>=<owner>/<repository>` pairs, comma-separated: where a run's workspace (or Vikunja project) works. Without a match and without `-repo`, GitHub is not read. |

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
`SILLAGE_TEST_DATABASE_URL`; without it they skip. CI's Go job sets it to a
PostgreSQL service, so `go test -race ./...` runs them there. Locally:

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
