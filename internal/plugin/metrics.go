package plugin

// This file turns a plugin's JSON answer into the text of one metric tile
// on its page (see Metric in manifest.go).

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"rasbeacon/internal/readings"
)

// ReadMetric picks a metric's value out of a JSON answer and formats it for
// the screen, e.g. "12,345", "8.1%", "1.2 GB" or "14 ms" (with a
// non-breaking space before the unit).
func ReadMetric(m Metric, body []byte) (string, error) {
	// "any" holds whatever the JSON is: a map for an object, a slice for a
	// list, float64 for a number, string, bool or nil.
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", errors.New("the answer is not JSON")
	}
	value, ok := lookup(doc, m.Path)
	if !ok {
		return "", fmt.Errorf("%s is not in the answer", m.Path)
	}
	if m.Format == "text" {
		text := fmt.Sprint(value)
		// A tile has room for a word or two, not a whole document.
		if len([]rune(text)) > 40 {
			text = string([]rune(text)[:39]) + "…"
		}
		return text, nil
	}
	n, ok := number(value)
	if !ok {
		return "", fmt.Errorf("%s is not a number", m.Path)
	}
	if m.DivideBy != "" {
		d, found := lookup(doc, m.DivideBy)
		dn, isNumber := number(d)
		if !found || !isNumber {
			return "", fmt.Errorf("%s is not a number", m.DivideBy)
		}
		// Nothing divided by nothing (no queries yet) shows as 0, not as an
		// error.
		if dn == 0 {
			n = 0
		} else {
			n /= dn
		}
	}
	if m.Scale != 0 {
		n *= m.Scale
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return "", errors.New("the number is too large")
	}
	switch m.Format {
	case "percent":
		return strconv.FormatFloat(n*100, 'f', 1, 64) + "%", nil
	case "bytes":
		// The same way as RasBeacon's other sizes ("14.2 MB").
		return readings.Bytes(uint64(math.Max(0, math.Round(n)))), nil
	case "ms":
		return trimNumber(n) + "\u00a0ms", nil
	case "seconds":
		return trimNumber(n) + "\u00a0s", nil
	}
	return groupThousands(n), nil
}

// lookup follows a dotted path: object keys, or list positions. For
// {"a": {"b": [5, 6]}}, "a.b.1" is 6.
func lookup(doc any, path string) (any, bool) {
	for _, part := range strings.Split(path, ".") {
		// A "type switch": v is doc as the type each case names.
		switch v := doc.(type) {
		case map[string]any:
			next, ok := v[part]
			if !ok {
				return nil, false
			}
			doc = next
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false
			}
			doc = v[i]
		default:
			return nil, false
		}
	}
	return doc, true
}

// number reads a JSON value as a number: numbers, numbers written as text
// ("12.5"), and true/false as 1/0.
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// trimNumber shows up to one decimal for small numbers, none for large.
func trimNumber(n float64) string {
	if math.Abs(n) >= 100 || n == math.Trunc(n) {
		return groupThousands(n)
	}
	return strconv.FormatFloat(n, 'f', 1, 64)
}

// groupThousands rounds and writes 12345 as "12,345". The rounded number is
// written as a float, not converted to an int64 first, which would
// overflow above about 9.2e18.
func groupThousands(n float64) string {
	if math.IsInf(n, 0) || math.IsNaN(n) {
		return strconv.FormatFloat(n, 'g', -1, 64)
	}
	s := strconv.FormatFloat(math.Round(n), 'f', 0, 64)
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		// A comma before every group of three digits, counted from the end.
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if negative {
		return "-" + b.String()
	}
	return b.String()
}
