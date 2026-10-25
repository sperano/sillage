package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/url"
	"os"
	"testing"
)

// responsesGolden holds the bodies the handlers answer the web client's
// requests with, besides stories and the overview (which have golden
// files of their own), keyed by path. The web client's tests decode each
// one, so a response shape changed here fails them until the client
// follows:
//
// go test ./internal/api -run TestResponsesGolden -update
const responsesGolden = "../../testdata/fixtures/api/responses.golden.json"

// goldenPerm is the mode of a rewritten golden file.
const goldenPerm = 0o644

var update = flag.Bool("update", false, "rewrite the API responses' golden file")

// contractPaths are the requests whose bodies responsesGolden holds: a
// search with a match and one without, a resolved link, and the sources'
// freshness.
var contractPaths = []string{
	"/api/v1/search?q=retry",
	"/api/v1/search?q=nothing-matches",
	"/api/v1/resolve?url=" + url.QueryEscape("https://github.example.com/example-org/widgets/pull/7"),
	"/api/v1/coverage",
}

func TestResponsesGolden(t *testing.T) {
	h := New(Config{Store: newFakeStore(t)})
	bodies := make(map[string]json.RawMessage, len(contractPaths))
	for _, p := range contractPaths {
		var body json.RawMessage
		decode(t, get(t, h, p), &body)
		bodies[p] = body
	}
	got, err := json.MarshalIndent(bodies, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *update {
		if err := os.WriteFile(responsesGolden, got, goldenPerm); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(responsesGolden)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("responses differ from %s (run with -update, then update web/src/lib/api.ts):\n%s",
			responsesGolden, got)
	}
}
