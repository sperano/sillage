package collector

import (
	"time"

	"github.com/sperano/sillage/internal/model"
)

// kelosGaps marks Kelos unknown where runs may be missing from a read,
// which lists only the Tasks still kept and the TaskRecords.
func (a *assembly) kelosGaps(recs model.Records) {
	st := a.sources[model.SourceKelos]
	if st == nil || st.cov.LastSuccessfulRead == nil {
		return
	}
	a.ttlGap(st, recs)
	for key, at := range a.announcedOnly {
		if at.Before(*st.cov.LastSuccessfulRead) {
			st.cov.Gaps = append(st.cov.Gaps, model.Gap{
				Start: at, End: *st.cov.LastSuccessfulRead,
				Reason: "run " + key.String() + " was announced, but no Task or TaskRecord of it was read: " +
					"its records are missing, from its announcement until whenever it ended",
			})
		}
	}
}

// ttlGap marks the time before the Task TTL: TaskRecords exist only for
// runs that reported usage, so from the story's earliest record to the
// read minus the TTL, runs may be missing.
func (a *assembly) ttlGap(st *sourceState, recs model.Records) {
	cutoff := st.cov.LastSuccessfulRead.Add(-a.cfg.KelosTaskTTL)
	earliest, ok := earliestTime(recs)
	if !ok || !earliest.Before(cutoff) {
		return
	}
	st.cov.Gaps = append(st.cov.Gaps, model.Gap{
		Start: earliest, End: cutoff,
		Reason: "Kelos deletes a finished Task after " + a.cfg.KelosTaskTTL.String() + " (" + EnvKelosTaskTTL +
			"): a run before then is read only from its TaskRecord, which only a run that reported usage leaves",
	})
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
