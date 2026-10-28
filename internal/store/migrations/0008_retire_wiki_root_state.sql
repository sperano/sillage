-- 0007 deleted the wiki checkpoints, but each root's collector state is a
-- checkpoint of the root's Vikunja instance (scope collector/roots/<ref>)
-- whose sources map kept its wiki entry. The loop turned that entry into a
-- wiki coverage that no longer validates, so every commit of the root
-- failed and the root was read again each cycle. Drop those entries.

UPDATE checkpoints c
SET value = jsonb_set(
        c.value::jsonb, '{sources}',
        COALESCE((SELECT jsonb_object_agg(e.key, e.value)
                  FROM jsonb_each(c.value::jsonb -> 'sources') AS e
                  WHERE e.value ->> 'source' IS DISTINCT FROM 'wiki'), '{}'::jsonb))::text
WHERE CASE WHEN c.scope LIKE 'collector/roots/%'
           THEN jsonb_path_exists(c.value::jsonb, '$.sources.* ? (@.source == "wiki")')
           ELSE false END;
