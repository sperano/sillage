# Sillage

A read-only history of what people, agents and automation did on a task,
across Vikunja, Kelos, GitHub and Wiki.js, on one timeline. *Sillage* is
French for the wake a boat leaves behind.

Sillage only observes: it is never in the path that starts agent work, so it
being down never blocks anything.

## Status

Pre-alpha. A Go binary that answers `GET /healthz` and serves a placeholder
page; the normalized model (`internal/model`) with fabricated fixture
stories (`testdata/fixtures/stories/`); a read-only client per source; and
`sillage story`, which assembles one story from the live sources and prints
it; and the frontend's single-task timeline graph (`web/src/lib/timeline/`),
rendered in tests from the fixture stories but not yet shown on the story
page. Nothing is stored, nothing is deployed.

## Layout

| Path | Contents |
| --- | --- |
| `cmd/sillage/` | The binary (collector, read API and embedded frontend, eventually). |
| `internal/model/` | Normalized events, runs, entities and coverage. |
| `internal/source/{vikunja,github,kelos,wiki}/` | One read-only source each. |
| `internal/collector/` | Joins the sources' records into one story. |
| `internal/store/` | Persistence. |
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
