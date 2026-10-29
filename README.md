# Sillage

A read-only history of what people, agents and automation did on a task,
across Vikunja, Kelos and GitHub, on one timeline. *Sillage* is
French for the wake a boat leaves behind.

Sillage only observes: it is never in the path that starts agent work, so it
being down never blocks anything.

## Status

Pre-alpha, deployed (from [`sperano/hollingsworth`](https://github.com/sperano/hollingsworth)).
A Go binary that answers `GET /healthz` and `GET /metrics`, serves the
frontend and the read-only JSON API under `/api/v1/`; the normalized model
(`internal/model`) with fabricated fixture stories
(`testdata/fixtures/stories/`); a read-only client per source; `sillage
story`, which assembles one story from the live sources and prints it; a
PostgreSQL store (`internal/store`); the collection loop, which keeps it
current; a webhook receiver (`internal/ingest`) whose deliveries make
the collection loop read the tasks they name; and the in-cluster endpoint of the phase markers Kelos runs' pods report
(`internal/ingest/marker`), stored, joined to their runs and paired into
the runs' phases in each story.
The story page shows a task's history: the timeline graph
(`web/src/lib/timeline/`), which draws the phases under their run's bar,
beside its event list and detail panel (`web/src/lib/story/`), under the
coverage notices and the next action. The overview page shows what was
working on what across tasks.

## Layout

| Path | Contents |
| --- | --- |
| `cmd/sillage/` | The binary: health check, metrics, read API, embedded frontend and the collector. |
| `internal/model/` | Normalized events, runs, entities and coverage. |
| `internal/source/{vikunja,github,kelos}/` | One read-only source each. |
| `internal/collector/` | Joins the sources' records into one story. |
| `internal/reconcile/` | The collection loop: reads the sources periodically into the store, with freshness and gaps; `pglock/`, its PostgreSQL lock. |
| `internal/store/` | PostgreSQL persistence, with its migrations embedded. |
| `internal/api/` | Read-only JSON API over the store. |
| `internal/ingest/` | Webhook receiver: Vikunja and GitHub deliveries, recorded as evidence, ask the collector for a fresh read. |
| `internal/ingest/marker/` | `POST /ingest/marker`: phase markers from Kelos runs' pods, authenticated by a TokenReview. |
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
| `/api/v1/stories/task/{instance}/{id}` | The stored `model.Story`, as the golden files under `testdata/fixtures/stories/` |
| `/api/v1/coverage/task/{instance}/{id}` | The task's coverage: last successful read and gaps per source |
| `/api/v1/coverage` | Per source instance, the newest and stalest last read across stored roots |
| `/api/v1/search?q=&limit=` | Roots by title or key, most recently active first (`q` at most 200 characters) |
| `/api/v1/resolve?url=` | The task stories that a Vikunja task, GitHub pull request or Kelos Task (`[<namespace>/]<name>`, UID) points to; 404 if none |
| `/api/v1/overview?from=&to=&project=&task=&executor=&review=&failed=&missing=&after=&limit=` | The tasks active in a window (default: the day before now, at most 31 days), one row each with its runs, pull request states, review waits, state changes and coverage; filtered, most recently active first, in pages of at most 200 (`after` is the previous page's `next`, sent with that page's `window` as `from` and `to`: a cursor of another window is a 400) |
| `/api/v1/version` | The binary's version |
| `/api/v1/config` | The frontend's configuration: the Kelos console's base URL per cluster name (`kelosConsoles`, empty without one) |

With `SILLAGE_KELOS_CONSOLE_URL` set (an http or https URL without a query
or fragment, for the cluster
`SILLAGE_KELOS_CLUSTER` names, which it then requires), the story page links
each run to its Task's logs in that Kelos console; unset, runs have no link. Every other `GET` path that is no frontend file
serves the frontend's `index.html`, so that deep links to its routes work,
unless its last segment has an extension (`/app.js`): a missing file is a
404.

`POST /ingest/github` and `POST /ingest/vikunja` (`internal/ingest`) take
the GitHub repository webhooks' and the Vikunja webhooks' deliveries. They
bypass the forward-auth, so each delivery's signature is their only
authentication: `X-Hub-Signature-256` under
`SILLAGE_GITHUB_WEBHOOK_SECRET`, `X-Vikunja-Signature` under
`SILLAGE_VIKUNJA_WEBHOOK_SECRET`. A source without a secret refuses every
delivery. A request that is too large (25 MB from GitHub, 5 MB from
Vikunja), unsigned or badly signed is refused, once its body, read up to
that limit, is checked (unread when it declares a larger one); nothing of
it is recorded or acted on. Each authenticated
attempt is recorded as evidence that sillage received it, not that Kelos
received or acted on its own delivery. It then asks the collector to read
the task it names again; nothing is written from the payload, and the
periodic reads stay authoritative. A delivery cuts the collection loop's
wait short: its next cycle starts `SILLAGE_DELIVERY_DELAY` (default `15s`)
later, so the deliveries of one change share it, and reads the tasks
deliveries named first, each once, at most
`SILLAGE_DELIVERY_ROOTS_PER_CYCLE` (default 25) of them, the others left
for the next cycle. Both paths answer 503 while this process does not
collect: without a store, while another process holds the collector lock,
or while the loop loads the store's state. `/metrics` counts the
deliveries by source and outcome (`sillage_webhook_deliveries_total`), the
reads they asked for (`sillage_collector_delivery_reads_total`: `caused`,
or `coalesced` into a read the cycle had due anyway) and the cycles they
started early (`sillage_collector_delivery_cycles_total`).

`POST /ingest/marker` (`internal/ingest/marker`) takes the phase markers
that wrappers in a Kelos run's pod report: one edge (`start` or `end`) of
one span (`blocker-wait`, `agent`), a JSON object of at most
4096 bytes of named fields only (when, which Task, how the span ended),
never a free-form payload such as a prompt or an output. It is meant for the in-cluster
Service only. Each request carries the pod's projected ServiceAccount
token (`Authorization: Bearer`, audience `sillage`), which sillage checks
with a TokenReview: its own ServiceAccount needs `create` on
`tokenreviews` (`system:auth-delegator`). Only ServiceAccounts of
`SILLAGE_KELOS_NAMESPACE` bound to a pod are accepted, and the pod the
review names is the marker's sender, whatever the body says. A verdict is
cached by the token's hash until the token expires (at most an hour). It
answers 202 when stored, 200 for an identical re-send, 409 for a different
marker under the same pod UID, span and edge, 429 past 2000 markers of one
pod, 400 for an invalid body (a marker more than 5 minutes from its
receipt included), 413 for one too large, 401 for a token refused, and 503
without a store, when the review or the store fails, or when too many
reviews or bodies are in flight for a second (try again later). A stored marker is
joined to its run by its pod, as an identifier, else by its Task name and
time inside exactly one run, as a heuristic, else it stays unattached
(counted in the stories' Kelos coverage); each later save of Kelos
records joins it again both ways, so a run saved with its Task name and
a matching interval attaches it even when no read ever names its pod.

A story pairs the markers joined to its runs into each run's `phases`
(`model.BuildStory`), by pod UID and span: both edges of one kind, the end
not before the start, make a complete phase; a start alone is in progress
while the run is unfinished and interrupted once it is not (never closed
at the run's end); an end alone has an unknown start; an end before its
start or of another kind leaves both edges with no interval. A phase
carries its edges' emitter, its pod and its join, and is uncertain when
either edge was joined by Task name and time. The Kelos coverage of a
story with phases notes that markers are best effort, and a restart's gap
on it that markers sent meanwhile may not have been received.

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

`sillage story` reads one task's records from the live sources, joins them,
and prints the story as JSON: the format of
`testdata/fixtures/stories/*/story.golden.json`. It writes nothing anywhere.

```sh
./sillage story -task <id>                 # a Vikunja task
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
| `SILLAGE_VIKUNJA_SERVICE_ACCOUNT` | Username that posts Kelos start announcements (default `gordon`). |
| `SILLAGE_VIKUNJA_AGENT_USERS` | Agents' Vikunja usernames, comma-separated: their comments are attributed to an agent. |
| `SILLAGE_KELOS_CLUSTER`, `SILLAGE_KELOS_NAMESPACE` | Name given to the cluster, and the namespace read; the Kubernetes credentials are the in-cluster ones, else the kubeconfig's current context. |
| `SILLAGE_KELOS_LABEL_DOMAIN`, `SILLAGE_KELOS_VIKUNJA_INSTANCE` | The spawners' label prefix, and the Vikunja host the task label names (must be `SILLAGE_VIKUNJA_URL`'s). |
| `SILLAGE_KELOS_TASK_TTL` | How long Kelos keeps a finished Task (default `72h`): runs that ended longer ago than that may be missing. |
| `SILLAGE_GITHUB_APP_ID`, `SILLAGE_GITHUB_APP_INSTALLATION_ID`, `SILLAGE_GITHUB_APP_PRIVATE_KEY` | The recommended GitHub credential: a read-only GitHub App installation. The key is the PEM itself (PKCS#1 `RSA PRIVATE KEY`, as GitHub generates it, or PKCS#8); sillage signs a short-lived JWT with it and exchanges it for an installation token, renewed before it expires. The installation has its own rate limit and, unlike a fine-grained token, Checks read. Grant it Contents, Pull requests, Issues, Checks and Metadata, all read-only; with `SILLAGE_GITHUB_HOOKS`, also Webhooks, read and write (write only to redeliver: read alone, lost deliveries are read again instead). |
| `SILLAGE_GITHUB_TOKEN`, `SILLAGE_GITHUB_API_URL` | A read-only personal access token instead of the App (set exactly one of the two credentials; for tests and development: a fine-grained token cannot read check runs of private repositories and shares its owner's rate limit), and the API (default `https://api.github.com`). |
| `SILLAGE_GITHUB_ANNOUNCERS` | GitHub logins whose pull request comments are parsed as start announcements, comma-separated. |
| `SILLAGE_WORKSPACES` | `<workspace>=<owner>/<repository>` pairs, comma-separated: where a run's workspace (or Vikunja project) works. Without a match and without `-repo`, GitHub is not read. |

## Collect into the store

With `SILLAGE_DATABASE_URL` set, the server also runs the collector: it
reads the sources with the settings above, assembles the story of every
root they name, and commits what changed to the store the API reads.

Each cycle reads the Kelos namespace once, and lists the tasks of the
configured Vikunja projects changed since the last listing. It reads a
root that is new, whose Kelos runs changed, whose task changed, or that
Kelos still names and that has a Kelos gap open or was not read for the
refresh interval. A root's GitHub side is read only when a webhook
delivery names the root, a run is new or changed, or the task changed;
otherwise its stored records stand. Once a day (the backstop) GitHub is
asked, with the last read's ETags, whether the root's pull requests
changed: `304 Not Modified` costs no rate limit and advances the
coverage, a change reads them whole. A root whose task is done, whose
pull requests are closed or merged and whose runs finished gets no
backstop unless its GitHub coverage has a gap open; no backstop read is
made while less than 20% of the rate limit's window is left, and a
restart spreads the overdue backstop reads over the interval. A story
whose records did not change writes none; its coverage still advances. Once a source of a root has been read
successfully, a failed read leaves a gap, from that source's last
successful read to its next, with the reason (`timeout`, `rate_limited`,
`server_error`, `failed`): a failed Kelos read, which serves every root,
leaves one on every root's Kelos coverage. A restart leaves one on every
source of every root that was read successfully before. A source never
read successfully gets no gap: nothing is known of it, and its coverage
has no last successful read. One process collects at a time, under a
PostgreSQL advisory lock: another reads nothing, says so in its log, and
tries again.

GitHub does not retry a delivery sillage did not answer with a 2xx (sillage,
the tunnel or the proxy down, a timeout). With `SILLAGE_GITHUB_HOOKS` set,
the first cycle after a start and then one cycle every
`SILLAGE_GITHUB_HOOK_CHECK_INTERVAL` read each hook's delivery log, newest
first, down to the newest attempt the last read handled (kept in the
store; a hook seen for the first time is read back one backstop interval),
so a quiet hook costs one request. An attempt that failed, and of whose
delivery (`X-GitHub-Delivery`) the store holds no accepted attempt, is
redelivered: the ingest path then records it and reads what it names. If
GitHub refuses (no Webhooks write), or the attempt was a redelivery
already, the roots its payload names are read in that cycle, and their
GitHub coverage gets a gap from when it was sent to that read, unless a
read since saw it. An attempt that could not be handled (GitHub or the
store failing) stops the read of that log, and the next one starts from it.
`sillage_collector_lost_deliveries_total` counts them by outcome:
`failed`, then `redelivered` or `unrecoverable`.

| Variable | Meaning |
| --- | --- |
| `SILLAGE_DATABASE_URL` | The store's PostgreSQL URL. Unset, the collector does not run. |
| `SILLAGE_VIKUNJA_PROJECTS` | Vikunja project ids whose changed tasks are read, comma-separated. Unset, only the tasks Kelos names are. |
| `SILLAGE_COLLECT_INTERVAL` | Time between two cycles (default `5m`). |
| `SILLAGE_ROOT_REFRESH_INTERVAL` | How long a root Kelos still names is left unread when nothing says it changed (default `1h`): task comments change no run. GitHub is not read then. |
| `SILLAGE_GITHUB_BACKSTOP_INTERVAL` | How long a root's GitHub side is left unread when no delivery, run or task change says it changed (default `24h`). |
| `SILLAGE_LOCK_RETRY_INTERVAL` | How often a process without the lock tries to take it (default `1m`). |
| `SILLAGE_GITHUB_HOOKS` | sillage's own GitHub repository webhooks, `owner/name:id`, comma-separated, whose delivery logs are read for the deliveries sillage did not receive. Unset, none is. The GitHub credential needs Webhooks read, and write to redeliver. |
| `SILLAGE_GITHUB_HOOK_CHECK_INTERVAL` | How long between two reads of those delivery logs (default `5m`); the first cycle after a start reads them. |

Every interval is spread by up to 10% either way. `GET /metrics` serves,
besides the Go runtime's and the process's, each source instance's last
successful read (`sillage_source_last_success_timestamp_seconds`), failed
reads by reason (`sillage_source_read_errors_total`) and read durations
(`sillage_source_read_duration_seconds`), the records committed
(`sillage_records_written_total`), store errors, cycles, roots, whether
this process holds the lock, the roots' GitHub reads by what asked for
them (`sillage_collector_github_reads_total`), the requests sent to
GitHub and those answered 304 (`sillage_github_requests_total`,
`sillage_github_not_modified_total`), the rate limit left
(`sillage_github_rate_limit_remaining`), and the phase-marker requests by outcome
(`sillage_phase_markers_total`). Who may reach it is the deployment's decision.

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

The store's, the collector lock's and the loop's store tests need a
PostgreSQL they may create schemas in, named by
`SILLAGE_TEST_DATABASE_URL`; without it they skip. CI's Go job sets it to a
PostgreSQL service, so `go test -race ./...` runs them there. Locally:

```sh
SILLAGE_TEST_DATABASE_URL=postgres://sillage@localhost:5432/sillage_test?sslmode=disable go test -race ./internal/store ./internal/reconcile/... ./cmd/sillage
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

The PostgreSQL tests run when `SILLAGE_TEST_DATABASE_URL` names a database
they may use, and are skipped otherwise.

## License

GNU General Public License v3.0; see [LICENSE](LICENSE).
