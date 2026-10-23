// Package testzone runs a package's tests in a time zone that is not UTC,
// so that output depending on the host's zone fails on every machine, not
// only on one set to another zone. It is imported by tests only.
package testzone

import (
	"testing"
	"time"
)

const (
	// name and offsetHours make the zone: seven hours west of UTC, as
	// America/Vancouver in summer. A fixed zone needs no tzdata.
	name        = "UTC-7"
	offsetHours = -7
)

// zone is the zone Main sets as time.Local.
var zone = time.FixedZone(name, offsetHours*int(time.Hour/time.Second))

// Main sets time.Local to the zone and runs the tests; call it from the
// package's TestMain. The zone is set before any test starts, so no
// goroutine reads time.Local while it changes. A time created before, by
// a package-level variable, keeps the host's zone.
func Main(m *testing.M) {
	time.Local = zone
	m.Run()
}
