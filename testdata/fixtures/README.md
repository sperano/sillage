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
