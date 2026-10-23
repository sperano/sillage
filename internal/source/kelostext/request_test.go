package kelostext

import (
	"slices"
	"testing"
)

func TestParseRequest(t *testing.T) {
	tests := []struct {
		text   string
		want   Request
		wantOK bool
	}{
		{"/gordon split the section", Request{Command: CommandGordon}, true},
		{"/kelos split the section", Request{Command: CommandKelos}, true},
		{"/gordon", Request{Command: CommandGordon}, true},
		{"/kelos", Request{Command: CommandKelos}, true},
		{"  /gordon address the review", Request{Command: CommandGordon}, true},
		{"/gordon @agent-sol tighten the steps", Request{Command: CommandGordon, Agent: "agent-sol"}, true},
		{"/kelos @agent-sol tighten the steps", Request{Command: CommandKelos, Agent: "agent-sol"}, true},
		{"/gordon @agent-sol, tighten the steps", Request{Command: CommandGordon, Agent: "agent-sol"}, true},
		{"/gordon @agent-sol: tighten the steps", Request{Command: CommandGordon, Agent: "agent-sol"}, true},
		{"/gordon @sol.", Request{Command: CommandGordon, Agent: "sol"}, true},
		{"/gordon @, tighten the steps", Request{Command: CommandGordon}, true},
		{"/gordon\tplease", Request{Command: CommandGordon}, true},
		{"/gordonx do it", Request{}, false},
		{"/kelosx do it", Request{}, false},
		{"please /gordon do it", Request{}, false},
		{"/Gordon do it", Request{}, false},
		{"Done.", Request{}, false},
		{"", Request{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseRequest(tt.text)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("ParseRequest(%q) = %+v, %v; want %+v, %v", tt.text, got, ok, tt.want, tt.wantOK)
		}
	}
}

// Commands lists both prefixes, the current one first, and hands out a
// copy.
func TestCommands(t *testing.T) {
	got := Commands()
	if want := []string{CommandGordon, CommandKelos}; !slices.Equal(got, want) {
		t.Fatalf("Commands() = %v, want %v", got, want)
	}
	got[0] = "/other"
	if Commands()[0] != CommandGordon {
		t.Error("Commands() shares its backing array")
	}
}
