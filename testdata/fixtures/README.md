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
  coverage), in any order, with a `description` of what the story shows.
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
   under `collector/<name>/` (see its README) and a case to `storyCases`
   in `internal/collector/collector_test.go`; `TestEveryStoryIsAssembled`
   fails until you do.
5. Add a row to the table above.
