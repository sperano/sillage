package model

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"strings"
	"time"
)

// fingerprintPrefix marks an event id as a fingerprint.
const fingerprintPrefix = "sha256:"

// fingerprintHexLen is the length of a hex-encoded SHA-256 digest.
const fingerprintHexLen = sha256.Size * 2

// Fingerprint derives an event id for a record its source gives no id,
// such as a Vikunja task's done_at or a wiki page's live version (absent
// from its history trail). It hashes:
//
//   - at: the source time, or, for a polled state with no source time, the
//     time it was first observed. A later repeat of the same action has a
//     different time, so it gets its own event rather than merging with
//     the first.
//   - object: the source object the action is on, e.g. "tasks/102" or
//     "pages/41".
//   - action: what happened to it, e.g. "done" or "edited".
//   - content: the distinguishing value, e.g. the bucket name; empty when
//     there is none.
//
// The time is hashed in UTC with nanoseconds, and each part is
// length-prefixed, so different splits of the same characters never
// collide. The result is "sha256:" followed by 64 hex digits.
func Fingerprint(at time.Time, object, action, content string) string {
	h := sha256.New() // a hash.Hash's Write never returns an error
	for _, part := range []string{at.UTC().Format(time.RFC3339Nano), object, action, content} {
		var n [binary.MaxVarintLen64]byte
		h.Write(n[:binary.PutUvarint(n[:], uint64(len(part)))])
		io.WriteString(h, part)
	}
	return fingerprintPrefix + hex.EncodeToString(h.Sum(nil))
}

func isFingerprint(id string) bool {
	digest, ok := strings.CutPrefix(id, fingerprintPrefix)
	if !ok || len(digest) != fingerprintHexLen {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil && strings.ToLower(digest) == digest
}
