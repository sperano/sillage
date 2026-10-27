-- Wiki.js is retired. Its page stories and records are not useful without
-- the source that refreshes and explains them, so remove them rather than
-- leaving permanently stale history in the task-only model.

DELETE FROM correlations
WHERE from_ref LIKE 'page:%'
   OR to_ref LIKE 'page:%'
   OR from_ref LIKE 'event:wiki/%'
   OR to_ref LIKE 'event:wiki/%';

DELETE FROM coverage WHERE source = 'wiki';
DELETE FROM checkpoints WHERE source = 'wiki';

DELETE FROM roots WHERE kind = 'page';
DELETE FROM events WHERE source = 'wiki';

DELETE FROM markers WHERE body->'report'->>'kind' = 'wiki-login';
DELETE FROM marker_limits l
WHERE NOT EXISTS (SELECT 1 FROM markers m WHERE m.pod_uid = l.pod_uid);

DELETE FROM run_origins WHERE origin->'annotations'->>'source' = 'wiki';
UPDATE runs
SET body = jsonb_set(body, '{role}', '"unknown"')
WHERE body->>'role' = 'wiki-follow-up';

DELETE FROM correlations c
WHERE NOT EXISTS (SELECT 1 FROM root_correlations m WHERE m.correlation_id = c.id);
DELETE FROM events e
WHERE NOT EXISTS (SELECT 1 FROM root_events m WHERE m.event_id = e.id);
DELETE FROM runs r
WHERE NOT EXISTS (SELECT 1 FROM root_runs m WHERE m.run_id = r.id);
DELETE FROM pull_requests p
WHERE NOT EXISTS (SELECT 1 FROM root_pull_requests m WHERE m.pull_request_id = p.id);

DROP INDEX events_supersedes;
DROP INDEX events_live_page;
ALTER TABLE events DROP COLUMN supersedes;
ALTER TABLE events DROP COLUMN live_page;

ALTER TABLE roots DROP COLUMN kind;
ALTER TABLE roots DROP COLUMN instance;
ALTER TABLE roots DROP COLUMN source_id;
ALTER TABLE roots DROP COLUMN locale;
