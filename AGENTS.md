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
| `cmd/sillage/` | The one binary: today a `GET /healthz` server that also serves the embedded frontend, and `sillage story`, which prints one story assembled from the live sources; later the collector loop, the read API and the frontend together. Flags, configuration, wiring and output only: what a story holds is `internal/collector`'s. |
| `internal/model/` | Normalized events, runs, entities and links, and coverage. Source-independent. |
| `internal/model/storytest/` | The invariant each fixture story exists for, asserted by the model's and the collector's tests. |
| `internal/collector/` | Assembles one story from the live sources: reads, correlations with their evidence and confidence, coverage. Read once, nothing kept. |
| `internal/source/vikunja/` | Read-only Vikunja client and its normalization into `model`. |
| `internal/source/github/` | Read-only GitHub client and its normalization into `model`. |
| `internal/source/kelos/` | Read-only Kelos Task and TaskRecord reader and its normalization into `model`. |
| `internal/source/wiki/` | Read-only Wiki.js client and its normalization into `model`. |
| `internal/source/htmltext/` | Rendered comment HTML to plain text, shared by the sources that parse comments. |
| `internal/source/kelostext/` | Requests (`/gordon`, `/kelos`) and Kelos start announcements in every form posted, shared by the sources that parse comments. |
| `internal/store/` | PostgreSQL persistence of `model` records, checkpoints and coverage; `migrations/` holds the numbered SQL files embedded and applied at startup (add one, never edit one that shipped). |
| `internal/api/` | The read-only HTTP API over `store`. |
| `testdata/fixtures/` | Fabricated source responses and stories, shared by Go and web tests. See its README. |
| `web/` | The frontend: Svelte 5 + Vite static single-page application in TypeScript (strict). `web/src/` is the app; `web/*.go` embed its build into the binary. |
| `web/placeholder/` | The committed page embedded by default, so Go builds without Node. |
| `Dockerfile`, `.dockerignore` | The one image: the `webdist` binary on `scratch`, `linux/amd64` and `linux/arm64`. See "The image" below. |
| `scripts/collect-licenses.sh` | Copies the licenses of what the binary links into the image's `/licenses`. |
| `.github/workflows/ci.yml` | CI: the gates below, verbatim. |

Deployment (Helm chart, values, ApplicationSet entry, database, network
policy, Authentik) lives in `sperano/hollingsworth`, not here; so do pushing
and pinning the image.

### The frontend in the binary

`go build` embeds `web/placeholder/` unless built with `-tags webdist`, which
embeds `web/dist/` (the output of `npm run build`) instead. A `webdist` build
without `web/dist/` fails to compile; it never falls back to the placeholder
silently. Release builds use `webdist`. `go.mod`'s `ignore ./web/node_modules`
keeps `./...` out of npm's tree.

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
Dockerfile passes the same flag. Agent pods have no Docker daemon: there,
run the stages' commands instead (`npm ci --ignore-scripts && npm run build`,
then `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags webdist
./cmd/sillage`).

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

The store's tests run against a real PostgreSQL named by
`SILLAGE_TEST_DATABASE_URL` (each test in its own schema) and skip without
it. Agent pods have none: say in the PR that they were skipped, or start a
local one (the zonky `embedded-postgres-binaries` jars on Maven Central
hold a relocatable PostgreSQL that runs without root).

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
Go tests check and the web tests are to render. To add one, follow "Adding a story" in
`testdata/fixtures/README.md`: write `input.json`, register an explicit
invariant check in `internal/model/storytest/invariants.go` (a test fails
until you do), then `go test ./internal/model -run TestStoryGolden -update`
and review the generated golden file. The collector must assemble the same
story from fabricated source responses (`testdata/fixtures/collector/`) and
keep its invariant; `TestEveryStoryIsAssembled` fails until it does. A
model change that alters a golden file is a contract change: regenerate,
read the diff, and say so in the PR.

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
- Named constants, not magic numbers.
- Files ≤ ~500 lines, functions ≤ ~50; compose small functions.
- Tests live in sibling files: `foo.go` → `foo_test.go`, `foo.ts` →
  `foo.test.ts`, a component `Foo.svelte` → `Foo.svelte.test.ts`.
