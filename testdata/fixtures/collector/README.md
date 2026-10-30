# Collector responses

Fabricated source responses from which `internal/collector`'s tests
assemble each story of `../stories/`, through the real clients: Vikunja and
GitHub answered by one `httptest` server routed by host, Kelos by client-go's
fake dynamic client (`internal/collector/collectortest`,
which also names each story's directories). Each assembled story must keep
the invariant of `internal/model/storytest` its story exists for.
`cmd/sillage`'s tests run the collection loop over the same responses.

One directory per story, one subdirectory per source it reads, each in its
source's own format (see `../vikunja/`, `../kelos/`, `../github/`), with
these differences:

- `vikunja/`: `task.json`, `comments-<page>.json`; one comment page.
- `kelos/`: `tasks.json` and `taskrecords.json` of the namespace `agents`
  of `cluster-a`, label domain `vikunja.example.com`. Task names follow the
  Hollingsworth spawners' conventions the collector reads.
- `github/`: `pulls.json` for the branch, then `<list>-<number>.json` and
  `check-runs-<sha>.json`, one page each, for the repository
  `example-org/widgets` (workspace `widgets`). A list without a file is
  empty. A file with a status before its extension
  (`check-runs-<sha>.403.json`) is a refused response: its body, answered
  with that status.
- `markers/`: `markers.json`, the phase markers the store joined to the
  story's runs (`model.Marker`, with its join). No source is read for
  them: the tests pass them to the collector as kept
  (`collectortest.Markers`), and `cmd/sillage`'s record them before the
  loop runs.

A source a story has no directory for must not be read: the fake fails the
test.

| Story | Root | What the collector must join |
| --- | --- | --- |
| `discuss-no-pr` | task 101 | The discussion run by its task label and announced pod; GitHub read for the branch and empty. An unrelated run of another task stays out. |
| `implementation-merged` | task 102 | The implementation run; PR 7 by its head branch; the merge bookkeeping Task by its branch and name (identifier); a PR follow-up known only from its TaskRecord, by its name (heuristic); the PR's opening to the run by its executor inside the run's interval. |
| `failed-run-retry` | task 104 | Two attempts of one Task name: the failed one known only from its TaskRecord, so its announcement is joined by time (heuristic), the retry by its pod. Two runs, never merged. |
| `announced-run-only` | task 105 | No Kelos object of the announced run: a run keyed by pod, its interval and outcome unknown, and a Kelos gap over it. No run label names a repository: the announced Task name's project does, so GitHub is read for the branch, and empty. |
| `checks-after-force-push` | task 106 | Its run known from its announcement alone, whose Task name's project names the repository: PR 61 by its head branch, with the checks of each commit pushed. |
| `second-pr-after-close` | task 108 | As above, the repository from the announced Task name: PR 62 closed, then PR 63 opened on the same branch. |
| `checks-forbidden` | task 110 | No record names a repository: PR 64 is found by looking for the branch in every mapped repository, joined by its head branch, with its review; the check runs of both its commits answered 403, so its checks are absent and GitHub's coverage notes them forbidden, the read still successful. |
| `reopened-task` | task 107 | No comment and no run: no record names a repository, so the branch is looked for in the one repository `SILLAGE_WORKSPACES` maps to, empty, and GitHub's coverage says so. |
| `phase-markers` | task 109 | The failed attempt from its TaskRecord, the retry from its running Task; the kept markers onto those two runs as phases, one of them joined by Task name and time (heuristic). In `cmd/sillage`, the markers recorded before the loop's first read join once it saves the runs. |
