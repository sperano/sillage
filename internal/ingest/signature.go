package ingest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

var (
	errUnsigned     = errors.New("the request is not signed")
	errBadSignature = errors.New("the signature does not match the body")
)

// verify checks that signature, the hex HMAC-SHA256 of body under secret
// after prefix, signs body. It compares in constant time.
func verify(secret Secret, prefix, signature string, body []byte) error {
	if signature == "" {
		return errUnsigned
	}
	digest, ok := strings.CutPrefix(signature, prefix)
	if !ok {
		return errBadSignature
	}
	got, err := hex.DecodeString(digest)
	if err != nil {
		return errBadSignature
	}
	if !hmac.Equal(got, sign(secret, body)) {
		return errBadSignature
	}
	return nil
}

// sign is the HMAC-SHA256 of body under secret.
func sign(secret Secret, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return mac.Sum(nil)
}
