-- The overview finds candidate roots from event kinds within a time window.
CREATE INDEX events_kind_at ON events (kind, at);
