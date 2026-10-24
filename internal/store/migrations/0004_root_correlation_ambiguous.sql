-- Whether a correlation is ambiguous in each story that holds it. The
-- collector decides it per story: an event on the root takes its run
-- candidates from the whole namespace, one on a linked page only from the
-- story's own runs, so one pair can be ambiguous in one story and certain
-- in another. Kept per root, it no longer depends on which story was saved
-- last; the correlation's body no longer carries it.
ALTER TABLE root_correlations ADD COLUMN ambiguous boolean NOT NULL DEFAULT false;
UPDATE root_correlations m SET ambiguous = COALESCE((c.body->>'ambiguous')::boolean, false)
    FROM correlations c WHERE c.id = m.correlation_id;
UPDATE correlations SET body = body - 'ambiguous';
