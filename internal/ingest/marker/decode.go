package marker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sperano/sillage/internal/model"
)

const (
	// MaxBodyBytes bounds a marker's body: one small JSON object.
	MaxBodyBytes = 4096
	// MaxSkew is how far a marker's time may be from its receipt. The
	// emitter sends at once, so a larger difference is a bug or a forgery.
	MaxSkew = 5 * time.Minute
)

var errTrailing = errors.New("the body holds more than one JSON object")

// decodeReport decodes one marker report, strictly: no unknown field, no
// trailing data, every value in its closed list, its time in UTC and
// within MaxSkew of received.
func decodeReport(body []byte, received time.Time) (model.MarkerReport, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var r model.MarkerReport
	if err := dec.Decode(&r); err != nil {
		return model.MarkerReport{}, fmt.Errorf("the body is no marker: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return model.MarkerReport{}, errTrailing
	}
	if err := r.Validate(); err != nil {
		return model.MarkerReport{}, err
	}
	if _, offset := r.At.Zone(); offset != 0 {
		return model.MarkerReport{}, errors.New("the marker's time is not in UTC")
	}
	if skew := r.At.Sub(received).Abs(); skew > MaxSkew {
		return model.MarkerReport{}, fmt.Errorf("the marker's time is %s from its receipt, more than %s",
			skew.Round(time.Second), MaxSkew)
	}
	r.At = r.At.UTC()
	return r, nil
}
