package testzone

import (
	"testing"
	"time"
)

func TestMain(m *testing.M) { Main(m) }

func TestLocalIsNotUTC(t *testing.T) {
	if time.Local != zone {
		t.Fatalf("time.Local is %v, want %v", time.Local, zone)
	}
	if _, offset := time.Now().Zone(); offset == 0 {
		t.Errorf("%v is UTC: a host-zone dependency would go unnoticed", zone)
	}
}
