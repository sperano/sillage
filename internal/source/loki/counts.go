package loki

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

const (
	// microDigits is the number of decimal places of a micro-dollar count.
	microDigits = 6
	// maxCostDigits bounds the decimal places of a cost written with an
	// exponent that is converted exactly.
	maxCostDigits = 30
)

var (
	decimalText  = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
	exponentText = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?[eE][+-]?[0-9]+$`)
	integerText  = regexp.MustCompile(`^[0-9]+$`)
)

// attrs is an entry's labels, read by name.
type attrs map[string]string

// count returns the non-negative integer attribute name, or nil when absent.
func (a attrs) count(name string) (*int64, error) {
	s, ok := a[name]
	if !ok || s == "" {
		return nil, nil
	}
	if !integerText.MatchString(s) {
		return nil, fmt.Errorf("%s %q is not a non-negative integer", name, s)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", name, s, err)
	}
	return &n, nil
}

// decimal returns the cost attribute name as a decimal string, or "" when
// absent. An exponent form is converted exactly or refused.
func (a attrs) decimal(name string) (string, error) {
	s := strings.TrimSpace(a[name])
	switch {
	case s == "":
		return "", nil
	case decimalText.MatchString(s):
		return s, nil
	case exponentText.MatchString(s):
		return exactDecimal(name, s)
	}
	return "", fmt.Errorf("%s %q is not a non-negative decimal", name, s)
}

func exactDecimal(name, s string) (string, error) {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return "", fmt.Errorf("%s %q is not a number", name, s)
	}
	out := r.FloatString(maxCostDigits)
	if back, _ := new(big.Rat).SetString(out); back.Cmp(r) != 0 {
		return "", fmt.Errorf("%s %q has more than %d decimal places", name, s, maxCostDigits)
	}
	if strings.Contains(out, ".") {
		out = strings.TrimRight(strings.TrimRight(out, "0"), ".")
	}
	return out, nil
}

// micros returns the integer attribute name, millionths of a dollar, as a
// decimal string, or "" when absent.
func (a attrs) micros(name string) (string, error) {
	s := strings.TrimSpace(a[name])
	if s == "" {
		return "", nil
	}
	if !integerText.MatchString(s) {
		return "", fmt.Errorf("%s %q is not a non-negative integer", name, s)
	}
	s = strings.TrimLeft(s, "0")
	if len(s) <= microDigits {
		s = strings.Repeat("0", microDigits+1-len(s)) + s
	}
	whole, frac := s[:len(s)-microDigits], strings.TrimRight(s[len(s)-microDigits:], "0")
	if frac == "" {
		return whole, nil
	}
	return whole + "." + frac, nil
}
