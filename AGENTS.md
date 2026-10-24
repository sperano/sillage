# AGENTS.md

Instructions for anyone, human or agent, changing this repository.

## What sillage is

Sillage is a read-only history of what people, agents and automation did on a
task, across Vikunja, Kelos, GitHub and Wiki.js: comments, runs, pull
requests, reviews, checks and wiki edits on one timeline, with the evidence
behind each record and the gaps where no source has data.

Sillage observes; it never takes part. It is never in the spawning path: no
webhook, spawner or agent waits on it, so a sillage outage must never block
agent work.

Status: pre-alpha. The repository is a skeleton; nothing is deployed.

## Layout

| Path | What goes there |
| --- | --- |
| `cmd/sillage/` | The one binary: a server of `GET /healthz`, `GET /metrics`, the read API under `/api/`, the webhook ingest paths under `/ingest/` (off, answering 503, until the collection loop drains the reads they ask for) and the embedded frontend (its `index.html` for every other `GET` path without a file extension, so client-side routes deep-link), `POST /ingest/marker` with a store, and, with `SILLAGE_DATABASE_URL` set, the collection loop into the store the API reads (`loopStore` adapts `store.Store` to `reconcile.Store`); and `sillage story`, which prints one story assembled from the live sources. Flags, configuration, wiring and output only: what a story holds is `internal/collector`'s, what the API answers `internal/api`'s, what a delivery does `internal/ingest`'s. |
| `internal/model/` | Normalized events, runs, entities and links, and coverage. Source-independent. |
| `internal/model/storytest/` | The invariant each fixture story exists for, asserted by the model's and the collector's tests. |
| `internal/testzone/` | `TestMain` for the packages with golden files: their tests run with `time.Local` seven hours west of UTC, so output that depends on the host's zone fails on every machine. |
| `internal/collector/` | Assembles one story from the live sources: reads, correlations with their evidence and confidence, coverage; and names the roots a Kelos read points to. Read once, nothing kept: a caller passes what a store kept back in. |
| `internal/source/vikunja/` | Read-only Vikunja client and its normalization into `model`. |
| `internal/source/github/` | Read-only GitHub client and its normalization into `model`. |
| `internal/source/kelos/` | Read-only Kelos Task and TaskRecord reader and watcher (list, then watch with persisted checkpoints), and their normalization into `model`, with the identifier correlations the spawners' correlation annotations (a Task's `Origin`) name. Each `run.observed` names the Task's pod at the time: a retried Job has two. |
| `internal/source/wiki/` | Read-only Wiki.js client and its normalization into `model`. |
| `internal/source/htmltext/` | Rendered comment HTML to plain text, shared by the sources that parse comments. |
| `internal/source/kelostext/` | Requests (`/gordon`, `/kelos`) and Kelos start announcements in every form posted, shared by the sources that parse comments. |
| `internal/reconcile/` | The collection loop: which roots to read and when, one Kelos read per cycle, freshness and gaps per root and source, the Prometheus metrics. Talks to the store through its `Store` and `Locker` interfaces. |
| `internal/reconcile/pglock/` | The `Locker` as a PostgreSQL session advisory lock: one process collects at a time. |
| `internal/reconcile/storetest/` | The contract of `reconcile.Store`, asserted of the loop tests' fake and, against PostgreSQL, of `cmd/sillage`'s adapter: a store the loop's tests do not model fails it first. |
| `internal/store/` | PostgreSQL persistence of `model` records, checkpoints, coverage, Kelos Task origins (kept on first sight: a TaskRecord does not copy annotations), every pod each run was seen with, and phase markers with their join to a run (by pod, else by Task name and time, else unattached and joined again by each save of Kelos records); `migrations/` holds the numbered SQL files embedded and applied at startup (add one, never edit one that shipped). |
| `internal/overview/` | The multi-task overview of a window: one row per task or page with its runs, pull requests, review waits and state changes in it, and whether its history is missing; filters and cursor pages. Pure: model records in, rows out. |
| `internal/api/` | The read-only JSON API over `store.Reader`: stories, coverage, search, link resolution, the overview, version. No authentication of its own: `/api/` must stay behind the Authentik forward-auth. Its tests serve every fixture story through a fixture-backed fake reader. |
| `internal/ingest/` | The webhook receiver: `POST /ingest/vikunja` and `POST /ingest/github`, authenticated by their signatures alone (they bypass the forward-auth). Each authenticated attempt is recorded in the store as evidence that sillage received it (`model.Delivery`), and asks the collector to read the roots it names again; no record comes from a payload. `Queue` is the collector loop's end of those requests. |
| `internal/ingest/marker/` | `POST /ingest/marker`, on the in-cluster Service only: the phase markers (`model.Marker`) that wrappers in a Kelos run's pod report, authenticated by a TokenReview of the pod's projected ServiceAccount token (audience `sillage`), the verdict cached by token hash until it expires. The sender is the reviewed pod, never the body. Stored as evidence, apart from every story; it asks for no read. |
| `testdata/fixtures/` | Fabricated source responses and stories, shared by Go and web tests; `overview/`, the overview's set of overlapping stories and its golden overview. See its README. |
| `web/` | The frontend: Svelte 5 + Vite static single-page application in TypeScript (strict). `web/src/` is the app; `web/*.go` embed its build into the binary. |
| `web/src/lib/` | The story types (`model.ts`), their key strings (`keys.ts`) and their runtime decoder, the read API client, the routes and the router. |
| `web/src/lib/selection.svelte.ts` | The selected event, run, check or review wait (`ItemRef`, `timeline.ts`), one instance shared by the timeline graph, the event list and the detail panel. |
| `web/src/lib/timeline/` | The single-task timeline graph (SVG): lanes, intervals, coverage shading, zoom and pan. Layout is pure TypeScript; the components draw it and handle zoom, pan, selection and group expansion. |
| `web/src/lib/overview/` | The multi-task overview: its types and decoder (the Go `overview.Page`), its URL query, what a row says at an instant and at the window's end, its lanes (by task or by executor) and the components that draw them. |
| `web/src/lib/testing/` | Web test helpers: the golden stories, record builders. |
| `web/src/pages/`, `web/src/components/` | One component per route, and the parts they share. |
| `web/placeholder/` | The committed page embedded by default, so Go builds without Node. |
| `Dockerfile`, `.dockerignore` | The one image: the `webdist` binary on `scratch`, `linux/amd64` and `linux/arm64`. See "The image" below. |
| `scripts/collect-licenses.sh` | Copies the licenses of what the binary links into the image's `/licenses`. |
| `.github/workflows/ci.yml` | CI: the gates below, verbatim (the Go job with a PostgreSQL service for the store's tests), and the image built for both platforms, never pushed. |

Deployment (Helm chart, values, ApplicationSet entry, database, network
policy, Authentik) lives in `sperano/hollingsworth`, not here; so do pushing
and pinning the image.

### The frontend in the binary

`go build` embeds `web/placeholder/` unless built with `-tags webdist`, which
embeds `web/dist/` (the output of `npm run build`) instead. A `webdist` build
without `web/dist/` fails to compile; it never falls back to the placeholder
silently. Release builds use `webdist`. `go.mod`'s `ignore ./web/node_modules`
keeps `./...` out of npm's tree.

### Routes

The frontend routes with the history API, not the hash, so that a link to
it is a plain URL: `/` (the entry page; `/?q=` a search), `/task/<instance>/<id>`,
`/page/<instance>/<id>/<locale>`, `/overview` (what was working on what, its
window, filters and grouping in the query), and `/open?url=<link>`, which resolves a
Vikunja, Kelos, GitHub or wiki link to its story. The server must answer
every `GET` path outside `/api/`, `/ingest/`, `/healthz` and `/metrics`
with `index.html` for those deep links to load.
The API client (`web/src/lib/api.ts`) makes one request per call, never
retries, and aborts what a navigation leaves behind.

### The image

```sh
docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=<version> -t sillage:dev .
```

The `web` and `go` stages run on the build platform (Go cross-compiles with
`GOARCH=$TARGETARCH`), so nothing runs under emulation. The runtime is
`scratch`: `/sillage` as `ENTRYPOINT`, CA certificates, and `/licenses`
(sillage's `LICENSE`, Go's and every linked module's from
`scripts/collect-licenses.sh`, and the bundled npm packages' from Vite's
`build.license`, `web/dist/.vite/license.md`). It runs as `65532:65532`,
needs no writable path, and listens on `:8080` (`SILLAGE_LISTEN_ADDR`).
`VERSION` sets `main.version` through `-ldflags -X`; the binary logs it at
startup and reports `dev` without it. `TestVersionLoggedAtStartup` checks
that `-X` reaches the variable, and `TestDockerfileSetsVersion` that the
Dockerfile passes the same flag. CI's `image` job builds both platforms
without pushing, then runs the amd64 image read-only and checks `/healthz`,
the built frontend, `/licenses`, a clean exit on SIGTERM and the logged
version. Agent pods have no Docker daemon: there, run the stages' commands
instead (`npm ci --ignore-scripts && npm run build`, then `CGO_ENABLED=0
GOOS=linux GOARCH=arm64 go build -tags webdist ./cmd/sillage`).

## Gates

Run the gate of every part you changed before committing; CI runs exactly
these commands.

Go, from the repository root:

```sh
test -z "$(gofmt -l $(git ls-files -co --exclude-standard '*.go'))"
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@2026.2.2 ./...
go test -race ./...
go build ./...
```

The store's, the collector lock's and `cmd/sillage`'s loop-store tests run
against a real PostgreSQL named by `SILLAGE_TEST_DATABASE_URL` and skip
without it. The store and loop-store tests each run in their own schema;
the lock tests take a random advisory-lock key each. CI's Go job runs them
against a PostgreSQL service. Agent pods have none: say in the PR that they
were skipped, or start a local one (the zonky `embedded-postgres-binaries`
jars on Maven Central hold a relocatable PostgreSQL that runs without root).

`gofmt -l .` would walk `web/node_modules` (gofmt ignores `go.mod`'s
`ignore`), hence the file list from git: tracked plus untracked, not ignored.

Web, from `web/`:

```sh
npm ci
npx playwright install --with-deps chromium   # CI only: agent images ship Chromium
npm run check    # svelte-check: TypeScript and Svelte diagnostics
npm run lint     # prettier --check, then eslint
npm test         # vitest: *.svelte.test.ts in Chromium, other *.test.ts in Node
npm run build    # vite build into web/dist
```

Then, from the repository root, with the frontend built:

```sh
go vet -tags=webdist ./... && go test -tags=webdist ./...
```

`playwright` is pinned to an exact version (`1.59.1`) whose Chromium build
matches the one in the agent image; upgrade both together.

## Fixtures

`testdata/fixtures/stories/` holds fabricated stories: normalized records
in `input.json`, and `story.golden.json`, the `model.BuildStory` output the
Go tests check and the web tests render (`web/src/lib/testing/goldens.ts`).
To add one, follow "Adding a story" in
`testdata/fixtures/README.md`: write `input.json`, register an explicit
invariant check in `internal/model/storytest/invariants.go` (a test fails
until you do), then `go test ./internal/model -run TestStoryGolden -update`
and review the generated golden file. The collector must assemble the same
story from fabricated source responses (`testdata/fixtures/collector/`) and
keep its invariant; `TestEveryStoryIsAssembled` fails until it does. A
model change that alters a golden file is a contract change: regenerate,
read the diff, and say so in the PR. The web tests decode every golden
story (`web/src/lib/decodeModel.test.ts`, which rejects unknown fields and
values) and render it (`web/src/pages/StoryPage.svelte.test.ts`), so a
change to the story JSON also changes `web/src/lib/model.ts` and
`web/src/lib/decodeModel.ts` in the same PR.

`testdata/fixtures/overview/` is the multi-task overview's set: stories of
several tasks with overlapping runs over one afternoon and the golden
overview of its window (`go test ./internal/overview -run
TestOverviewGolden -update`). The API serves it and the web tests decode
and render it, so a change to the overview's JSON also changes
`web/src/lib/overview/` in the same PR.

## Rules

- **The repository is public.** Fixtures, examples, tests, comments and docs
  use fabricated data only: no real Vikunja ids or URLs, task titles, wiki
  paths, tokens or hostnames, only placeholders (`*.example.com`). See
  `testdata/fixtures/README.md`.
- **Sources are read-only.** No source client writes to Vikunja, GitHub,
  Kubernetes or Wiki.js, and credentials are requested with read scopes
  only.
- **A run is identified by cluster + namespace + Task UID**, never by Task
  name or branch: a retry reuses both, and must stay a separate run.
- **An unknown interval stays unknown.** A gap between two records is never
  drawn or counted as work, waiting or idle time. A correlation the evidence
  does not establish is shown as uncertain, not as a causal link.
- **A time read from a source never takes the host's zone.** A decoder that
  puts times in `time.Local` (Kubernetes' `metav1.Time` does) has its times
  converted to UTC; `model.BuildStory` turns every time into UTC.
- Named constants, not magic numbers.
- Files ≤ ~500 lines, functions ≤ ~50; compose small functions.
- Tests live in sibling files: `foo.go` → `foo_test.go`, `foo.ts` →
  `foo.test.ts`, a component `Foo.svelte` → `Foo.svelte.test.ts`.
