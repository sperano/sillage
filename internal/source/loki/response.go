package loki

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// queryResponse is Loki's query_range answer.
type queryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string         `json:"resultType"`
		Result     []streamResult `json:"result"`
	} `json:"data"`
}

type streamResult struct {
	Stream map[string]string `json:"stream"`
	// Values are [timestamp, line] and, with the categorize-labels
	// encoding, a third element holding the structured metadata.
	Values [][]json.RawMessage `json:"values"`
}

// entry is one log entry with its labels: the stream's, and the
// structured metadata merged over them.
type entry struct {
	at     time.Time
	labels map[string]string
}

// entries returns the answer's entries, or an error when it is not a
// successful streams result.
func (r queryResponse) entries() ([]entry, error) {
	if r.Status != "success" || r.Data.ResultType != "streams" {
		return nil, fmt.Errorf("loki: unexpected answer: status %q, result type %q", r.Status, r.Data.ResultType)
	}
	var out []entry
	for _, s := range r.Data.Result {
		for _, v := range s.Values {
			e, err := decodeValue(s.Stream, v)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
	}
	return out, nil
}

func decodeValue(stream map[string]string, v []json.RawMessage) (entry, error) {
	const minElements = 2
	if len(v) < minElements {
		return entry{}, fmt.Errorf("loki: a value has %d elements, want at least %d", len(v), minElements)
	}
	var ts string
	if err := json.Unmarshal(v[0], &ts); err != nil {
		return entry{}, fmt.Errorf("loki: a value's timestamp: %w", err)
	}
	ns, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return entry{}, fmt.Errorf("loki: a value's timestamp: %w", err)
	}
	labels := make(map[string]string, len(stream))
	for k, val := range stream {
		labels[k] = val
	}
	if len(v) > minElements {
		var extra struct {
			StructuredMetadata map[string]string `json:"structuredMetadata"`
		}
		if err := json.Unmarshal(v[minElements], &extra); err != nil {
			return entry{}, fmt.Errorf("loki: a value's metadata: %w", err)
		}
		for k, val := range extra.StructuredMetadata {
			labels[k] = val
		}
	}
	return entry{at: time.Unix(0, ns).UTC(), labels: labels}, nil
}
