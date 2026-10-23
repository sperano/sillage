-- At most one accepted attempt per delivery: a later attempt of a delivery
-- accepted already is recorded as ignored, so that the roots it names are
-- read again once per delivery, however often it is redelivered or
-- replayed.
CREATE UNIQUE INDEX delivery_attempts_accepted ON delivery_attempts (source, instance, delivery_id)
    WHERE outcome = 'accepted';
