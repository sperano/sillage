# GitHub responses

Fabricated REST responses for the repository `example-org/widgets`, read by
`internal/source/github`'s tests through an `httptest` server. One directory
per story. A list's page `<p>` is a file of its own; the server sends a
`Link: rel="next"` header while the next page's file exists.

- `pulls-<p>.json`: `GET /repos/{owner}/{repo}/pulls?state=all&head=…`;
- `pull-<n>.json`: `GET /repos/{owner}/{repo}/pulls/{n}`;
- `timeline-<n>-<p>.json`: `GET /repos/{owner}/{repo}/issues/{n}/timeline`;
- `comments-<n>-<p>.json`: `GET /repos/{owner}/{repo}/issues/{n}/comments`;
- `review-comments-<n>-<p>.json`: `GET /repos/{owner}/{repo}/pulls/{n}/comments`
  (inline comments on the diff);
- `reviews-<n>-<p>.json`: `GET /repos/{owner}/{repo}/pulls/{n}/reviews`;
- `check-runs-<sha>-<p>.json`: `GET /repos/{owner}/{repo}/commits/{sha}/check-runs?filter=all`;
- `records.golden.json`: what `Normalize` makes of the story's pull
  requests, events and checks. Generated; never edit it by hand:
  `go test ./internal/source/github -run TestNormalizeGolden -update`,
  then review the diff.

The tests request two items per page.

| Story | Read by | What it covers |
| --- | --- | --- |
| `no-pr` | branch `kelos/vikunja-4300` | No pull request on the branch. |
| `two-prs` | branch `kelos/vikunja-4301` | Two pull requests on one branch: #21 closed, reopened and closed again unmerged, #22 merged (its close at the merge is dropped). A fork's branch of the same name (#23) and another branch (#24) are listed but skipped. One agent label at creation names #21's executor; two agents' labels leave #22's unknown. A `/kelos` request in the conversation and one inline on the diff, an inline comment dated by its review's submission, a bot comment, a team review request and its removal, an approval and a pending review (skipped). |
| `closed-unmerged` | pull request 31 | Opened by another person, changes requested, closed unmerged; a cancelled check. |
| `checks-two-shas` | pull request 41 | Checks on two commits: the first commit's failure is kept beside the head's results; a re-run check keeps both attempts; an in-progress and a queued check stay pending with an open end. A second agent label, added an hour later, names nobody. |
| `follow-up-announced` | pull request 51 | Start announcements in every form Kelos posted, by the owner's account (a PAT Workspace) or an App's bot: follow-ups in the current, colon and lowercase forms (the last with a linked Task and the namespace), a conflict run in the current form. `/gordon @<agent>`, inline `/gordon` and `/kelos` requests. An announcement naming another pull request's Task, and one posted by an account not listed as an announcer, stay plain comments. The tests list `owner-gh` and `example-agents[bot]` as announcers. |
