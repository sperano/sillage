# Wiki.js responses

Fabricated Wiki.js 2 GraphQL replies, read by `internal/source/wiki`'s tests
through an `httptest` server. One directory per story. Each file is a whole
reply (`data`, and `errors` where the story needs one) to one operation:

- `page.json`: `pages.single(id)`, the live page with its author and creator;
- `pages.json`: `pages.list`, read only when `page.json` answers `Forbidden`
  (a token without `manage:pages`/`write:pages`);
- `history-<p>.json`: `pages.history(id, offsetPage: <p>, offsetSize: 2)`,
  newest first, with Wiki.js's own `actionType`;
- `version-<v>.json`: `pages.version(pageId, versionId: <v>)`, the newest
  trail version's path;
- `comments.json`: `comments.list(locale, path)`, with `render` (HTML), not
  `content`, which needs `write:comments`;
- `records.golden.json`: what `Normalize` makes of the story. Generated;
  never edit it by hand:
  `go test ./internal/source/wiki -run TestNormalizeGolden -update`,
  then review the diff.

Users: `Robin Owner` (id 7), `Agent Opus` (id 21) and `Agent Sol` (id 22),
the last two listed as agent users.

| Story | Page | What it covers |
| --- | --- | --- |
| `live-edit-not-in-trail` | 57 | The trail holds the creation's version and one edit; the newest edit is only the live page's `updatedAt` and author, and becomes an edit with a fingerprint id. A `/kelos` request, its start announcement and the agent's reply. |
| `two-agents` | 58 | Two agents, each started by its own `/kelos @<agent>` request and announcement, edit the page in turn; the owner moves it in between (a move with an unknown actor). Six versions over three trail pages, the oldest of which Wiki.js reports as a move from `""`. |
| `announcement-no-reply` | 59 | A request and its announcement with no reply after it. Read with a token refused `pages.single`: the live page comes from `pages.list`, the live edit's author is unknown, and the creator is the oldest version's author. A person quoting an announcement, and an agent's announcement naming another page, stay plain comments. |
