package collector

import (
	"time"

	"github.com/sperano/sillage/internal/model"
)

// kelosGaps returns where runs may be missing from a Kelos read, which
// lists only the Tasks still kept and the TaskRecords: before the Task
// TTL, and from each announcement no Kelos object was found for.
func (a *assembly) kelosGaps(cov model.Coverage, recs model.Records) []model.Gap {
	read := cov.LastSuccessfulRead
	if read == nil {
		return nil
	}
	var gaps []model.Gap
	if g, ok := a.ttlGap(*read, recs); ok {
		gaps = append(gaps, g)
	}
	for key, at := range a.announcedOnly {
		if at.Before(*read) {
			gaps = append(gaps, model.Gap{
				Start: at, End: *read,
				Reason: "run " + key.String() + " was announced, but no Task or TaskRecord of it was read: " +
					"its records are missing, from its announcement until whenever it ended",
			})
		}
	}
	return gaps
}

// ttlGap is the time before the Task TTL: TaskRecords exist only for runs
// that reported usage, so from the story's earliest record to the read
// minus the TTL, runs may be missing.
func (a *assembly) ttlGap(read time.Time, recs model.Records) (model.Gap, bool) {
	cutoff := read.Add(-a.cfg.KelosTaskTTL)
	earliest, ok := earliestTime(recs)
	if !ok || !earliest.Before(cutoff) {
		return model.Gap{}, false
	}
	return model.Gap{
		Start: earliest, End: cutoff,
		Reason: "Kelos deletes a finished Task after " + a.cfg.KelosTaskTTL.String() + " (" + EnvKelosTaskTTL +
			"): a run before then is read only from its TaskRecord, which only a run that reported usage leaves",
	}, true
}

// earliestTime is the earliest time any record of recs holds.
func earliestTime(recs model.Records) (time.Time, bool) {
	var earliest time.Time
	consider := func(t time.Time, ok bool) {
		if ok && (earliest.IsZero() || t.Before(earliest)) {
			earliest = t
		}
	}
	for _, e := range recs.Events {
		consider(e.Time(), true)
	}
	for _, r := range recs.Runs {
		if r.Created != nil {
			consider(*r.Created, true)
		}
		consider(r.Interval.Start.Time())
		consider(r.Interval.End.Time())
	}
	return earliest, !earliest.IsZero()
}
