package ingest

import (
	"errors"
	"testing"
)

func TestVerify(t *testing.T) {
	body := []byte(`{"a":1}`)
	good := hexHMAC(testGitHubSecret, body)
	tests := []struct {
		name      string
		prefix    string
		signature string
		body      []byte
		want      error
	}{
		{"signed", githubSignaturePrefix, githubSignaturePrefix + good, body, nil},
		{"no prefix expected", "", good, body, nil},
		{"unsigned", githubSignaturePrefix, "", body, errUnsigned},
		{"missing prefix", githubSignaturePrefix, good, body, errBadSignature},
		{"another algorithm", githubSignaturePrefix, "sha1=" + good, body, errBadSignature},
		{"not hex", githubSignaturePrefix, githubSignaturePrefix + "zz", body, errBadSignature},
		{"truncated", githubSignaturePrefix, githubSignaturePrefix + good[:10], body, errBadSignature},
		{"tampered body", githubSignaturePrefix, githubSignaturePrefix + good, []byte(`{"a":2}`), errBadSignature},
		{"another secret", "", hexHMAC("other", body), body, errBadSignature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := verify(testGitHubSecret, tt.prefix, tt.signature, tt.body); !errors.Is(err, tt.want) {
				t.Errorf("verify() = %v, want %v", err, tt.want)
			}
		})
	}
}
