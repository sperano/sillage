-- A correlation's confidence and evidence in each story that holds it, not
-- only whether it is ambiguous (0004). The collector writes the candidate
-- count into an evidence note, so two stories sharing a pair (a task's and
-- another task's with one shared pod) overwrote each other's note: one
-- could show the link as ambiguous next to the other's "no other run was
-- running". root_correlations.body holds the story's correlation without
-- its ends, ambiguous only when it is (as the model writes it); the
-- correlation's body keeps only the ends, {from, to}, which repointing a
-- run changes for every story at once.
ALTER TABLE root_correlations ADD COLUMN body jsonb;
UPDATE root_correlations m
    SET body = CASE WHEN m.ambiguous
                    THEN jsonb_set(c.body - 'from' - 'to', '{ambiguous}', 'true'::jsonb)
                    ELSE c.body - 'from' - 'to' - 'ambiguous' END
    FROM correlations c WHERE c.id = m.correlation_id;
ALTER TABLE root_correlations ALTER COLUMN body SET NOT NULL;
ALTER TABLE root_correlations DROP COLUMN ambiguous;
UPDATE correlations SET body = jsonb_build_object('from', body -> 'from', 'to', body -> 'to');
