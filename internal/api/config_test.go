package api

import (
	"maps"
	"net/http"
	"testing"
)

func TestConfig(t *testing.T) {
	consoles := map[string]string{"cluster-a": "https://kelos.example.com"}
	var got ConfigResponse
	decode(t, get(t, New(Config{KelosConsoles: consoles}), "/api/v1/config"), &got)
	if !maps.Equal(got.KelosConsoles, consoles) {
		t.Errorf("kelosConsoles = %v, want %v", got.KelosConsoles, consoles)
	}
}

func TestConfigWithoutConsoles(t *testing.T) {
	rec := get(t, New(Config{}), "/api/v1/config")
	if body := rec.Body.String(); rec.Code != http.StatusOK || body != `{"kelosConsoles":{}}` {
		t.Errorf("status %d, body %q; want 200 and an empty map, not null", rec.Code, body)
	}
}
