package model

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var fingerprintFormat = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func TestFingerprintFormat(t *testing.T) {
	got := Fingerprint(at(0), "tasks/42", "done", "")
	if !fingerprintFormat.MatchString(got) {
		t.Errorf("Fingerprint() = %q, want sha256: followed by %d lowercase hex digits", got, fingerprintHexLen)
	}
	if !isFingerprint(got) {
		t.Errorf("isFingerprint(%q) = false, want true", got)
	}
}

func TestFingerprintDeterministic(t *testing.T) {
	a := Fingerprint(at(0), "tasks/42", "done", "bucket")
	b := Fingerprint(at(0), "tasks/42", "done", "bucket")
	if a != b {
		t.Errorf("same inputs gave %q and %q", a, b)
	}
}

func TestFingerprintDiffersPerPart(t *testing.T) {
	base := Fingerprint(at(0), "tasks/42", "done", "bucket")
	tests := []struct {
		name string
		got  string
	}{
		{"time", Fingerprint(at(0).Add(time.Nanosecond), "tasks/42", "done", "bucket")},
		{"object", Fingerprint(at(0), "tasks/43", "done", "bucket")},
		{"action", Fingerprint(at(0), "tasks/42", "undone", "bucket")},
		{"content", Fingerprint(at(0), "tasks/42", "done", "other")},
		{"empty content", Fingerprint(at(0), "tasks/42", "done", "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got == base {
				t.Errorf("changing the %s did not change the fingerprint", tt.name)
			}
		})
	}
}

func TestFingerprintLengthPrefixed(t *testing.T) {
	tests := []struct {
		name string
		a, b [3]string // object, action, content
	}{
		{"object and action", [3]string{"ab", "c", ""}, [3]string{"a", "bc", ""}},
		{"action and content", [3]string{"o", "ab", "c"}, [3]string{"o", "a", "bc"}},
		{"object and content", [3]string{"ab", "", "c"}, [3]string{"a", "", "bc"}},
		{"moving a separator-like byte", [3]string{"a/b", "c", ""}, [3]string{"a", "b/c", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Fingerprint(at(0), tt.a[0], tt.a[1], tt.a[2])
			b := Fingerprint(at(0), tt.b[0], tt.b[1], tt.b[2])
			if a == b {
				t.Errorf("splits %q and %q collide on %q", tt.a, tt.b, a)
			}
		})
	}
}

func TestFingerprintSameInstantAcrossZones(t *testing.T) {
	zones := []*time.Location{time.UTC, testEST, time.FixedZone("CEST", 2*60*60)}
	want := Fingerprint(at(0), "pages/41", "edited", "")
	for _, zone := range zones {
		if got := Fingerprint(at(0).In(zone), "pages/41", "edited", ""); got != want {
			t.Errorf("zone %v: got %q, want %q", zone, got, want)
		}
	}
}

func TestIsFingerprint(t *testing.T) {
	valid := Fingerprint(at(0), "tasks/42", "done", "")
	digest := strings.TrimPrefix(valid, fingerprintPrefix)
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"valid", valid, true},
		{"empty", "", false},
		{"prefix only", fingerprintPrefix, false},
		{"no prefix", digest, false},
		{"wrong prefix", "sha1:" + digest, false},
		{"upper-case prefix", "SHA256:" + digest, false},
		{"too short", fingerprintPrefix + digest[1:], false},
		{"too long", fingerprintPrefix + digest + "0", false},
		{"upper-case digest", fingerprintPrefix + strings.ToUpper(digest), false},
		{"non-hex", fingerprintPrefix + strings.Repeat("g", fingerprintHexLen), false},
		{"non-hex last digit", fingerprintPrefix + digest[:fingerprintHexLen-1] + "z", false},
		{"surrounding space", " " + valid, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFingerprint(tt.id); got != tt.want {
				t.Errorf("isFingerprint(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
