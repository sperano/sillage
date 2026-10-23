package collector

import (
	"strings"
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestActorNoteNamesEveryAgentCounted(t *testing.T) {
	e := model.Event{ID: "pages/41", Actor: model.Account{Login: "Agent Opus", Kind: model.ActorAgent}}
	note := actorNote(e, []string{"Agent Opus", testAgent}, 2)
	if want := "2 runs of Agent Opus or " + testAgent + " were running"; !strings.Contains(note, want) {
		t.Errorf("actorNote() = %q, want it to say %q", note, want)
	}
}
