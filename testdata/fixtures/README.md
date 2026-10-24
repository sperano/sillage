# Fixtures

Saved source responses and the normalized stories built from them. Both the
Go collector tests and the web UI tests read this directory, so one corpus
covers normalization and rendering.

**Every fixture is fabricated.** This repository is public. Never commit a
real response, even a "redacted" one. Use placeholders only:

- hosts: `vikunja.example.com`, `wiki.example.com`, `github.example.com`
  (RFC 2606 `example.com` / `.example` / `.test` names);
- Vikunja task and comment ids, Wiki.js page and comment ids, PR numbers,
  Kubernetes UIDs: invented values, not copied from a real system;
- task titles, comment and page text, wiki paths, user names: invented;
- tokens, passwords, signatures, cookies: never, not even expired ones.

When a fixture reproduces something seen in production, rewrite every value
above and keep only the shape and the timing that matter for the test.

## Stories

`stories/<name>/` holds one fabricated story, the shared contract between
the source adapters, the store and the web UI:

- `input.json`: the story's root (a Vikunja task, or a wiki page for work
  requested from the wiki) and its normalized records (`internal/model`
  `Records`: events, runs, pull requests with their checks, correlations,
  coverage, and the phase markers joined to the runs, each with the join
  the store makes of it), in any order, with a `description` of what the
  story shows.
  Times may carry any offset.
- `story.golden.json`: `model.BuildStory`'s output for it: validated,
  every time in UTC, every list in its deterministic order. Generated;
  never edit it by hand.

The Go tests in `internal/model` build each story, compare it with its
golden file, rebuild it from shuffled inputs to prove the order never
depends on the input's, check that every correlation points into the
story, and assert the invariant the story exists for:

| Story | Invariant |
| --- | --- |
| `discuss-no-pr` | A `discuss` task delivers a wiki page and a reply: no pull request, no coding-phase run, and GitHub coverage shows it was read. |
| `implementation-merged` | The PR's merge (GitHub) and the task's completion (Vikunja) are two events; checks keep a failure on the first commit and a success on the second; the PR's account (a person) and executor (an agent) stay apart. |
| `wiki-rooted-request` | A story can be rooted in a page with no task; the live page version, absent from the history trail, is an edit with a fingerprint id; an edit two concurrent runs could have made keeps both candidates, flagged ambiguous. |
| `failed-run-retry` | A retry with the same Task name and a new UID is a second run, never merged with the failed one. |
| `announced-run-only` | A run known only from its start announcement is keyed by pod, its interval and outcome stay unknown despite a later agent comment, and Kelos coverage marks the period unknown. |
| `checks-after-force-push` | Checks from three observed GitHub heads remain in the story across a force push; a later head never replaces an earlier SHA's results. |
| `reopened-task` | After completion, reopening and a second completion, Vikunja exposes only the latest `done_at`; the story states that limitation and invents no earlier event. |
| `second-pr-after-close` | A closed-unmerged pull request followed by a second pull request on the same branch does not complete the task. |
| `phase-markers` | A run's phases come only from its markers: a complete pair is known to known, a start alone is in progress while the run runs and interrupted once it ended (never closed at its end), an end alone has an unknown start, an end before its start or of another kind keeps both edges with no interval; a marker joined by Task name and time makes its phase uncertain. The store joins the markers as the fixture does. |

The multi-task overview reads the same corpus: `internal/overview`
asserts what each story's row shows (`corpusCases`) and sweeps every hour
of its day; `internal/store` saves the corpus and checks that the overview
built from what `Activity` reads is the one built from the files, hour by
hour; and `cmd/sillage` runs the collection loop over each story's
`collector/` responses into PostgreSQL and checks what `GET
/api/v1/overview` serves against the stored stories.

## Overview

`overview/` holds the multi-task overview's set: seven fabricated tasks over
one afternoon (2026-03-10, the window 14:00 to 15:00 UTC), one story file
each under `overview/stories/` (`input.json`'s fields plus a `title`), and
`overview.golden.json`, the overview of that window, unfiltered, on one
page. The Go tests in `internal/overview` build it, compare it with the
golden file and assert each point explicitly; `internal/store` saves the
set and checks that the overview it reads back is the same; `internal/api`
serves it; the web tests decode and render the golden file. These stories
are not in `stories/`: they exist for overlap across tasks, not for one
story's invariant, and the collector does not assemble them.

| Task | What it shows |
| --- | --- |
| 201 | A run waiting 30 s before it starts, a pull request, a review request unanswered at the last read. |
| 202 | The same, ending before 14:25; its pull request closed after the window with the request unanswered. |
| 203 | A failed run alongside 201's run, under the same agent profile; its retry, same Kelos Task name, still running. |
| 204 | A pull request closed without a merge; the task is never marked done. |
| 205 | A run known from its announcement only, inside a Kelos gap: missing history. |
| 206 | Ran before the window; approved, merged and marked done in it. |
| 207 | The day before: no row. |

At 14:25, 201, 203 and 204 are executing. To change the set, edit the
story files and run `go test ./internal/overview -run TestOverviewGolden
-update`, then read the golden file's diff.

Each source's raw, fabricated responses live in its own directory
(`vikunja/`, `kelos/`, `github/`, `wiki/`), and the responses from which
the collector assembles each story in `collector/`.

### Adding a story

1. Create `stories/<name>/input.json` (copy the closest story). Fabricate
   every value, as above. Event ids are the source's own ids, or, when it
   has none, `model.Fingerprint` of the documented parts.
2. Add `<name>` to `invariants` in
   `internal/model/storytest/invariants.go`, with a check function that
   asserts the story's point explicitly; `TestEveryFixtureHasInvariants`
   fails until you do.
3. Generate the golden file and read it before committing:
   `go test ./internal/model -run TestStoryGolden -update`.
4. Make the collector assemble the story from source responses: add them
   under `collector/<name>/` (see its README) and an entry to `Stories`
   in `internal/collector/collectortest/stories.go`;
   `TestEveryStoryIsAssembled` fails until you do.
5. Say what the overview must show of it: a case in `corpusCases` in
   `internal/overview/corpus_test.go`;
   `TestEveryCorpusStoryHasAnOverviewCase` fails until you do.
6. Add a row to the table above.
