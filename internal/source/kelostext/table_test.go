package kelostext

import (
	"maps"
	"testing"
)

func TestLocationRows(t *testing.T) {
	tests := []struct {
		name string
		text string
		want map[string]string
	}{
		{
			name: "rendered HTML, every label",
			text: "KELOS TASK\ntask-a\nJOB UID\nuid-a\nPOD\npod-a\nNODE\nnode-a",
			want: map[string]string{labelTask: "task-a", labelJob: "uid-a", labelPod: "pod-a", labelNode: "node-a"},
		},
		{
			name: "Markdown, aligned separator and padded cells",
			text: "\n|            |        |\n|:-----------|-------:|\n|  KELOS TASK  |  task-a  |\n| POD | pod-a |",
			want: map[string]string{labelTask: "task-a", labelPod: "pod-a"},
		},
		{
			name: "stops at the first line past the table",
			text: "KELOS TASK\ntask-a\nWANT TO WATCH?\nPOD\npod-a",
			want: map[string]string{labelTask: "task-a"},
		},
		{
			name: "Markdown, stops at a row of another table",
			text: "| KELOS TASK | task-a |\n| STATUS | ok | extra |\n| POD | pod-a |",
			want: map[string]string{labelTask: "task-a"},
		},
		{
			name: "a label seen twice keeps its first value",
			text: "KELOS TASK\ntask-a\nKELOS TASK\ntask-b",
			want: map[string]string{labelTask: "task-a"},
		},
		{
			name: "a label without its value",
			text: "KELOS TASK\ntask-a\nPOD",
			want: map[string]string{labelTask: "task-a"},
		},
		{name: "no table", text: "COFFEE TIME!", want: map[string]string{}},
		{name: "empty", text: "", want: map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := locationRows(tt.text); !maps.Equal(got, tt.want) {
				t.Errorf("locationRows = %v, want %v", got, tt.want)
			}
		})
	}
}
