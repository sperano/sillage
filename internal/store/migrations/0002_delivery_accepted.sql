-- At most one accepted attempt recorded per delivery: a later attempt of a
-- delivery accepted already is recorded as ignored. Store.AcceptDelivery
-- inserts the accepted attempt in the transaction that asks for the reads
-- of the roots it names, so that only one attempt asks for them, however
-- often the delivery is redelivered.
CREATE UNIQUE INDEX delivery_attempts_accepted ON delivery_attempts (source, instance, delivery_id)
    WHERE outcome = 'accepted';
