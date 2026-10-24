package model

import (
	"strings"
	"testing"
	"time"
)

func testDelivery() Delivery {
	return Delivery{
		Source:     SourceGitHub,
		Instance:   "github.example.com",
		ID:         "delivery-1",
		Event:      "check_run",
		ReceivedAt: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		Outcome:    DeliveryAccepted,
	}
}

func TestDeliveryValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Delivery)
		want   string
	}{
		{"valid", func(*Delivery) {}, ""},
		{"no event", func(d *Delivery) { d.Event = "" }, ""},
		{"unknown source", func(d *Delivery) { d.Source = "jira" }, "known source"},
		{"no instance", func(d *Delivery) { d.Instance = "" }, "an instance"},
		{"no id", func(d *Delivery) { d.ID = "" }, "an id"},
		{"no time", func(d *Delivery) { d.ReceivedAt = time.Time{} }, "time it was received"},
		{"unknown outcome", func(d *Delivery) { d.Outcome = "duplicate" }, "unknown outcome"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := testDelivery()
			tt.change(&d)
			err := d.Validate()
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("Validate() = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func TestDeliveryOutcomeValid(t *testing.T) {
	for _, o := range []DeliveryOutcome{DeliveryAccepted, DeliveryIgnored, DeliveryRejected, DeliveryFailed} {
		if !o.Valid() {
			t.Errorf("%q.Valid() = false", o)
		}
	}
	if DeliveryOutcome("").Valid() {
		t.Error(`"".Valid() = true`)
	}
}
