package testzone

import (
	"testing"
	"time"
)

func TestMain(m *testing.M) { Main(m) }

func TestLocalIsNotUTC(t *testing.T) {
	if time.Local != Zone {
		t.Fatalf("time.Local is %v, want %v", time.Local, Zone)
	}
	if _, offset := time.Now().Zone(); offset == 0 {
		t.Errorf("%v is UTC: a host-zone dependency would go unnoticed", Zone)
	}
}
