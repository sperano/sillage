package model

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// eventContractFile is the event contract the web decoder is checked
// against (web/src/lib/decodeModel.test.ts):
// go test ./internal/model -run TestEventContractGolden -update
const eventContractFile = "../../testdata/fixtures/contract/event.golden.json"

// eventContract is what an Event's JSON may hold: every kind with the
// sources it can come from, the kinds of a state seen by polling, and
// every field with whether it may be absent.
type eventContract struct {
	Kinds         map[EventKind][]Source `json:"kinds"`
	ObservedKinds []EventKind            `json:"observedKinds"`
	Fields        []contractField        `json:"fields"`
}

type contractField struct {
	Name     string `json:"name"`
	Optional bool   `json:"optional"`
}

func buildEventContract(t *testing.T) eventContract {
	t.Helper()
	c := eventContract{Kinds: kindSources}
	for k := range observedKinds {
		c.ObservedKinds = append(c.ObservedKinds, k)
	}
	slices.Sort(c.ObservedKinds)
	typ := reflect.TypeFor[Event]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			t.Fatalf("Event.%s has no JSON name: the contract cannot name it", f.Name)
		}
		optional := slices.Contains(strings.Split(opts, ","), "omitempty")
		c.Fields = append(c.Fields, contractField{Name: name, Optional: optional})
	}
	return c
}

// TestEventContractGolden keeps the event contract file in step with the
// model, so that a kind or field added here fails the web tests until
// the decoder takes it.
func TestEventContractGolden(t *testing.T) {
	got, err := json.MarshalIndent(buildEventContract(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *update {
		if err := os.WriteFile(eventContractFile, got, goldenPerm); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(eventContractFile)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("event contract differs from %s (run with -update, then update the web model):\n%s",
			eventContractFile, got)
	}
}
