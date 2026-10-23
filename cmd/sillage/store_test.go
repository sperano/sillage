package main

import (
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/store"
)

func TestOpenStoreWithoutDatabase(t *testing.T) {
	st, err := openStore(t.Context(), func(string) string { return "" })
	if st != nil || err != nil {
		t.Errorf("openStore = %v, %v; want no store and no error", st, err)
	}
}

func TestOpenStoreErrors(t *testing.T) {
	const password = "s3cret-password"
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"bad retention", map[string]string{
			store.EnvDatabaseURL: "postgres://sillage:" + password + "@127.0.0.1:1/sillage",
			store.EnvRetention:   "soon",
		}},
		{"unreachable", map[string]string{
			store.EnvDatabaseURL: "postgres://sillage:" + password + "@127.0.0.1:1/sillage?connect_timeout=1",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := openStore(t.Context(), func(k string) string { return tt.env[k] })
			if err == nil {
				st.Close()
				t.Fatal("openStore succeeded")
			}
			if strings.Contains(err.Error(), password) {
				t.Errorf("error %q contains the password", err)
			}
		})
	}
}
