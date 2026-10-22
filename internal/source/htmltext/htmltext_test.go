package htmltext

import "testing"

func TestPlain(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"paragraphs and breaks", "<p>A &amp; <code>B</code></p>\n<p>  C\n  D <br/>E</p>", "A & B\nC D\nE"},
		{"upper-case tags", "<P>A</P ><BR>B", "A\nB"},
		{"empty paragraphs", "<p></p><p> </p><p>A</p>", "A"},
		{"carriage returns", "A\r\nB", "A B"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Plain(tt.body); got != tt.want {
				t.Errorf("Plain(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}
