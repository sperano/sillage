package model

import "testing"

func TestCorrelationValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Correlation)
		want   string
	}{
		{"valid identifier", func(*Correlation) {}, ""},
		{"valid heuristic", func(c *Correlation) { c.Confidence = ConfidenceHeuristic }, ""},
		{"valid ambiguous", func(c *Correlation) { c.Ambiguous = true }, ""},
		{
			"self-correlation",
			func(c *Correlation) { c.To = c.From },
			"cannot correlate with itself",
		},
		{
			"same id, different kinds",
			func(c *Correlation) { c.To = Ref{Kind: RefEvent, ID: c.From.ID} },
			"",
		},
		{"unknown confidence", func(c *Correlation) { c.Confidence = "certain" }, `unknown confidence "certain"`},
		{"empty confidence", func(c *Correlation) { c.Confidence = "" }, "unknown confidence"},
		{"no evidence", func(c *Correlation) { c.Evidence = nil }, "evidence is required"},
		{
			"invalid evidence",
			func(c *Correlation) { c.Evidence = []Evidence{{Source: SourceKelos}} },
			"evidence needs",
		},
		{"from with unknown kind", func(c *Correlation) { c.From.Kind = "comment" }, "unknown kind"},
		{"from without id", func(c *Correlation) { c.From.ID = "" }, "id is required"},
		{"to with unknown kind", func(c *Correlation) { c.To.Kind = "" }, "unknown kind"},
		{"to without id", func(c *Correlation) { c.To.ID = "" }, "id is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testCorrelation()
			tt.mutate(&c)
			checkErr(t, c.Validate(), tt.want)
		})
	}
}

func TestCorrelationValidateReportsBothRefs(t *testing.T) {
	c := testCorrelation()
	c.From.ID, c.To.Kind = "", "bogus"
	err := c.Validate()
	checkErr(t, err, "id is required")
	checkErr(t, err, "unknown kind")
}

func TestCorrelationString(t *testing.T) {
	const want = "run:cluster-a/agents/uid/uid-1 -> task:vikunja.example.com/tasks/42"
	if got := testCorrelation().String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestCorrelationValidateNamesCorrelation(t *testing.T) {
	c := testCorrelation()
	c.Confidence = ""
	checkErr(t, c.Validate(), "correlation run:cluster-a/agents/uid/uid-1 -> task:")
}

func TestCorrelationPairKey(t *testing.T) {
	c := testCorrelation()
	reversed := Correlation{From: c.To, To: c.From}
	if c.PairKey() != reversed.PairKey() {
		t.Errorf("PairKey() = %q one way, %q the other; want one key", c.PairKey(), reversed.PairKey())
	}
	// Joined unquoted, both pairs would read "event:x <-> event:y <-> event:z".
	event := func(id string) Ref { return Ref{Kind: RefEvent, ID: id} }
	a := Correlation{From: event("x <-> event:y"), To: event("z")}
	b := Correlation{From: event("x"), To: event("y <-> event:z")}
	if a.PairKey() == b.PairKey() {
		t.Errorf("PairKey() = %q for two different pairs", a.PairKey())
	}
}
