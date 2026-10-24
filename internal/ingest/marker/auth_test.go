package marker

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestTokenExpiry(t *testing.T) {
	exp := testNow.Add(time.Hour)
	if got, ok := tokenExpiry(fabricatedToken("pod", exp)); !ok || !got.Equal(exp) {
		t.Errorf("expiry = %s %v, want %s", got, ok, exp)
	}
	noExp := "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"pod"}`)) + ".c2ln"
	for name, token := range map[string]string{
		"no expiry": noExp, "two parts": "e30.e30", "a payload not base64": "e30.!!!.c2ln",
		"a payload not JSON": "e30." + base64.RawURLEncoding.EncodeToString([]byte("exp")) + ".c2ln",
	} {
		if _, ok := tokenExpiry(token); ok {
			t.Errorf("%s: an expiry was read", name)
		}
	}
}

// A token without an expiry it claims is reviewed every time.
func TestTokenWithoutExpiryIsNotCached(t *testing.T) {
	hs := newHarness(t)
	token := "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"pod"}`)) + ".c2ln"
	hs.reviewer.statuses[token] = podStatus(testNamespace)
	hs.post(token, body())
	hs.post(token, body())
	if hs.reviewer.reviews != 2 {
		t.Errorf("%d reviews, want 2", hs.reviewer.reviews)
	}
}

func TestVerdictCache(t *testing.T) {
	c := newVerdictCache(2)
	id := identity{namespace: testNamespace, serviceAccount: "default", pod: testPod, podUID: testPodUID}
	key := func(b byte) tokenKey { return tokenKey{b} }
	c.put(key(1), id, testNow.Add(time.Minute), testNow)
	c.put(key(2), id, testNow.Add(time.Hour), testNow)
	c.put(key(3), id, testNow.Add(time.Hour), testNow)
	if _, ok := c.get(key(3), testNow); ok {
		t.Error("a full cache took a verdict")
	}
	later := testNow.Add(2 * time.Minute)
	if _, ok := c.get(key(1), later); ok {
		t.Error("an expired verdict was returned")
	}
	c.put(key(3), id, testNow.Add(time.Hour), later)
	if got, ok := c.get(key(3), later); !ok || got != id {
		t.Error("a verdict was not cached once an expired one left")
	}
	c.put(key(4), id, testNow, testNow)
	if _, ok := c.get(key(4), testNow); ok {
		t.Error("a verdict already expired was cached")
	}
}

// The verdict of a token claiming a far expiry is cached for at most
// maxVerdictLifetime.
func TestVerdictLifetimeIsBounded(t *testing.T) {
	hs := newHarness(t)
	token := fabricatedToken("long", testNow.Add(365*24*time.Hour))
	hs.reviewer.statuses[token] = podStatus(testNamespace)
	hs.post(token, body())
	hs.clock = testNow.Add(maxVerdictLifetime)
	hs.post(token, body())
	if hs.reviewer.reviews != 2 {
		t.Errorf("%d reviews, want the verdict expired after %s", hs.reviewer.reviews, maxVerdictLifetime)
	}
}
