-- The roots each webhook delivery attempt named: those it asked the
-- collector to read, or, for a later attempt of a delivery accepted
-- before, those its accepted attempt named. A root is its model.Ref
-- string, as roots.ref, since a delivery can name a root not stored yet.
-- read_at is when the first successful read of the root from the
-- attempt's source instance that started at or after received_at, and
-- was saved after the attempt was recorded, started (saving the root's
-- coverage sets it); NULL until one did.
CREATE TABLE delivery_roots (
    attempt_id bigint NOT NULL REFERENCES delivery_attempts (id) ON DELETE CASCADE,
    root_ref   text NOT NULL,
    read_at    timestamptz,
    PRIMARY KEY (attempt_id, root_ref)
);
CREATE INDEX delivery_roots_root ON delivery_roots (root_ref);
