package model

import (
	"errors"
	"fmt"
	"time"
)

// DeliveryOutcome is what sillage did with a webhook delivery attempt.
type DeliveryOutcome string

// The outcomes of a delivery attempt sillage authenticated. A request
// that fails authentication is no delivery: nothing is recorded of it.
const (
	// DeliveryAccepted: sillage asked the collector to read the roots the
	// delivery names again. It says nothing of what that read found.
	DeliveryAccepted DeliveryOutcome = "accepted"
	// DeliveryIgnored: the delivery names no root sillage reads, or an
	// earlier attempt of it was accepted.
	DeliveryIgnored DeliveryOutcome = "ignored"
	// DeliveryRejected: the payload could not be read.
	DeliveryRejected DeliveryOutcome = "rejected"
	// DeliveryFailed: sillage could not ask for the read.
	DeliveryFailed DeliveryOutcome = "failed"
)

// Valid reports whether o is one of the outcomes above.
func (o DeliveryOutcome) Valid() bool {
	switch o {
	case DeliveryAccepted, DeliveryIgnored, DeliveryRejected, DeliveryFailed:
		return true
	}
	return false
}

// Delivery is one webhook delivery attempt that sillage received from a
// source. It is evidence that sillage received it, at ReceivedAt, and of
// nothing else: not that the source's event happened then, not that
// Kelos (or any other subscriber) received its own delivery of the same
// event, and not that anything acted on it. It is kept apart from the
// story's events: the story's records come only from reading the source.
type Delivery struct {
	Source   Source `json:"source"`
	Instance string `json:"instance"`
	// ID is the source's delivery id (GitHub's X-GitHub-Delivery). A
	// redelivery reuses it. Vikunja sends none, so its deliveries are
	// identified by the SHA-256 of their body, which carries the time
	// Vikunja sent it: a retry of the same send has the same id.
	ID string `json:"id"`
	// Event is the source's event name (GitHub's X-GitHub-Event, Vikunja's
	// event_name), empty when the payload could not be read.
	Event      string          `json:"event,omitempty"`
	ReceivedAt time.Time       `json:"receivedAt"`
	Outcome    DeliveryOutcome `json:"outcome"`
	// Detail says why the delivery was ignored, rejected or failed. It
	// never quotes the payload.
	Detail string `json:"detail,omitempty"`
}

// Validate reports whether the delivery names its source, instance, id,
// time and a known outcome.
func (d Delivery) Validate() error {
	var errs []error
	if !d.Source.Valid() || d.Instance == "" || d.ID == "" {
		errs = append(errs, fmt.Errorf("delivery %q: a known source, an instance and an id are required", d.ID))
	}
	if d.ReceivedAt.IsZero() {
		errs = append(errs, fmt.Errorf("delivery %q: the time it was received is required", d.ID))
	}
	if !d.Outcome.Valid() {
		errs = append(errs, fmt.Errorf("delivery %q: unknown outcome %q", d.ID, d.Outcome))
	}
	return errors.Join(errs...)
}
