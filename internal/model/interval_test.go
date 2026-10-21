package model

import (
	"testing"
	"time"
)

func TestBoundValidate(t *testing.T) {
	now := at(0)
	tests := []struct {
		name  string
		bound Bound
		end   bool
		want  string
	}{
		{"known start", KnownAt(now), false, ""},
		{"known end", KnownAt(now), true, ""},
		{"known without time", Bound{State: BoundKnown}, false, "needs a time"},
		{"unknown start", Unknown(), false, ""},
		{"unknown end", Unknown(), true, ""},
		{"unknown with time", Bound{State: BoundUnknown, At: &now}, true, "has no time"},
		{"open end", Open(), true, ""},
		{"open start", Open(), false, "only an end can be open"},
		{"open with time", Bound{State: BoundOpen, At: &now}, true, "has no time"},
		{"unknown state", Bound{State: "maybe"}, true, "unknown bound state"},
		{"empty state", Bound{}, false, "unknown bound state"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkErr(t, tt.bound.validate(tt.end), tt.want) })
	}
}

func TestBoundTime(t *testing.T) {
	now := at(0)
	tests := []struct {
		name   string
		bound  Bound
		want   time.Time
		wantOK bool
	}{
		{"known", KnownAt(now), now, true},
		{"unknown", Unknown(), time.Time{}, false},
		{"open", Open(), time.Time{}, false},
		{"known without time", Bound{State: BoundKnown}, time.Time{}, false},
		{"unknown carrying a time", Bound{State: BoundUnknown, At: &now}, time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.bound.Time()
			if ok != tt.wantOK || !got.Equal(tt.want) {
				t.Errorf("Time() = %v, %v, want %v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestIntervalValidate(t *testing.T) {
	tests := []struct {
		name string
		iv   Interval
		want string
	}{
		{"known to known", Interval{Start: KnownAt(at(0)), End: KnownAt(at(5))}, ""},
		{"zero length", Interval{Start: KnownAt(at(0)), End: KnownAt(at(0))}, ""},
		{"known to open", Interval{Start: KnownAt(at(0)), End: Open()}, ""},
		{"known to unknown", Interval{Start: KnownAt(at(0)), End: Unknown()}, ""},
		{"unknown to known", Interval{Start: Unknown(), End: KnownAt(at(5))}, ""},
		{"unknown to unknown", Interval{Start: Unknown(), End: Unknown()}, ""},
		{"end before start", Interval{Start: KnownAt(at(5)), End: KnownAt(at(0))}, "ends before it starts"},
		{
			"end before start across zones",
			Interval{Start: KnownAt(at(5)), End: KnownAt(atEST(0))},
			"ends before it starts",
		},
		{"open start", Interval{Start: Open(), End: Open()}, "interval start: only an end can be open"},
		{"known start without time", Interval{Start: Bound{State: BoundKnown}, End: Open()}, "interval start"},
		{"invalid end", Interval{Start: Unknown(), End: Bound{State: "maybe"}}, "interval end"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkErr(t, tt.iv.Validate(), tt.want) })
	}
}

func TestIntervalDuration(t *testing.T) {
	const span = 7 * time.Minute
	tests := []struct {
		name   string
		iv     Interval
		want   time.Duration
		wantOK bool
	}{
		{"both known", Interval{Start: KnownAt(at(0)), End: KnownAt(at(0).Add(span))}, span, true},
		{"zero length", Interval{Start: KnownAt(at(0)), End: KnownAt(at(0))}, 0, true},
		{"across zones", Interval{Start: KnownAt(atEST(0)), End: KnownAt(at(0).Add(span))}, span, true},
		{"negative", Interval{Start: KnownAt(at(0).Add(span)), End: KnownAt(at(0))}, -span, true},
		{"unknown start", Interval{Start: Unknown(), End: KnownAt(at(0))}, 0, false},
		{"unknown end", Interval{Start: KnownAt(at(0)), End: Unknown()}, 0, false},
		{"open end", Interval{Start: KnownAt(at(0)), End: Open()}, 0, false},
		{"unknown start and open end", Interval{Start: Unknown(), End: Open()}, 0, false},
		{"both unknown", Interval{Start: Unknown(), End: Unknown()}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.iv.Duration()
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("Duration() = %v, %v, want %v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestIntervalNormalize(t *testing.T) {
	iv := Interval{Start: KnownAt(atEST(0)), End: Open()}
	iv.normalize()
	if got := iv.Start.At.Location(); got != time.UTC {
		t.Errorf("start location = %v, want UTC", got)
	}
	if !iv.Start.At.Equal(at(0)) {
		t.Errorf("start = %v, want the same instant as %v", iv.Start.At, at(0))
	}
	if iv.End.At != nil {
		t.Errorf("open end gained a time: %v", iv.End.At)
	}
}
