package htmltext

import "testing"

func TestPlain(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"paragraphs and breaks", "<p>A &amp; <code>B</code></p>\n<p>  C\n  D <br/>E</p>", "A & B\nC D\nE"},
		{"upper-case tags", "<P>A</P ><BR>B", "A\nB"},
		{"empty paragraphs", "<p></p><p> </p><p>A</p>", "A"},
		{"headings and lists", "<h2>Title</h2>Lead<ul><li>A</li><li>B</li></ul>", "Title\nLead\nA\nB"},
		{"divs and quotes", "<div class=\"x\">A</div><blockquote>B</blockquote>C<hr>D", "A\nB\nC\nD"},
		{"block tag, inline tags sharing a prefix", "<pre>A</pre><b>B</b><br>C<pa>D</pa>", "A\nB\nCD"},
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
