package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"github.com/sperano/sillage/internal/model"
)

// digest identifies records apart from when they were read: two reads of
// an unchanged story have the same digest. Coverage is left out, and so is
// every event's observed time; an event whose source gives it neither an
// id nor a time (a state seen by polling) is identified by what it says,
// since its fingerprint holds when it was seen.
func digest(recs model.Records) string {
	var lines []string
	add := func(kind string, v any) {
		data, err := json.Marshal(v)
		if err != nil {
			panic("reconcile: a record does not marshal: " + err.Error())
		}
		lines = append(lines, kind+" "+string(data))
	}
	for _, e := range recs.Events {
		e.ObservedAt = time.Time{}
		if e.IDKind == model.IDFingerprint && e.SourceTime == nil {
			e.ID = ""
		}
		add("event", e)
	}
	for _, r := range recs.Runs {
		add("run", r)
	}
	for _, p := range recs.PullRequests {
		add("pr", p)
	}
	for _, c := range recs.Correlations {
		add("correlation", c)
	}
	slices.Sort(lines)
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
