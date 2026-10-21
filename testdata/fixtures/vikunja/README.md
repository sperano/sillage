# Vikunja responses

Fabricated `/api/v1` responses, read by `internal/source/vikunja`'s tests
through an `httptest` server. One directory per story:

- `task.json`: `GET /api/v1/tasks/{id}?expand=buckets`;
- `comments-<n>.json`: `GET /api/v1/tasks/{id}/comments?page=<n>`.

The tests request two comments per page, so a page of two is full.

| Story | Task | What it covers |
| --- | --- | --- |
| `two-pages` | 4201 | A full page, then a short one; a start announcement; an edited comment; a done task with `done_at`. |
| `one-full-page` | 4202 | Exactly one full page: the client must read page 2, which is empty. |
| `no-comments` | 4203 | No comments, labels, assignees or buckets. |
| `unparseable-announcement` | 4204 | The service account's start announcement without a Kelos Task: a plain comment, flagged. |
