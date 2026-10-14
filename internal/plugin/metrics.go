package plugin

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ReadMetric picks a metric's value out of a JSON answer and formats it for
// the screen, e.g. "12,345", "8.1%", "1.2 GB" or "14 ms".
func ReadMetric(m Metric, body []byte) (string, error) {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("the answer is not JSON")
	}
	value, ok := lookup(doc, m.Path)
	if !ok {
		return "", fmt.Errorf("%s is not in the answer", m.Path)
	}
	if m.Format == "text" {
		return fmt.Sprint(value), nil
	}
	n, ok := number(value)
	if !ok {
		return "", fmt.Errorf("%s is not a number", m.Path)
	}
	if m.DivideBy != "" {
		d, ok := lookup(doc, m.DivideBy)
		dn, isNumber := number(d)
		if !ok || !isNumber {
			return "", fmt.Errorf("%s is not a number", m.DivideBy)
		}
		if dn == 0 {
			n = 0
		} else {
			n /= dn
		}
	}
	if m.Scale != 0 {
		n *= m.Scale
	}
	switch m.Format {
	case "percent":
		return strconv.FormatFloat(n*100, 'f', 1, 64) + "%", nil
	case "bytes":
		return formatBytes(n), nil
	case "ms":
		return trimNumber(n) + " ms", nil
	case "seconds":
		return trimNumber(n) + " s", nil
	}
	return groupThousands(n), nil
}

// lookup follows a dotted path: object keys, or list positions.
func lookup(doc any, path string) (any, bool) {
	for _, part := range strings.Split(path, ".") {
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

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
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

// groupThousands rounds and writes 12345 as "12,345".
func groupThousands(n float64) string {
	s := strconv.FormatInt(int64(math.Round(n)), 10)
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
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

// formatBytes writes a size in decimal units, as the rest of the UI does.
func formatBytes(n float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	unit := 0
	for n >= 1000 && unit < len(units)-1 {
		n /= 1000
		unit++
	}
	if unit == 0 || n >= 100 {
		return strconv.FormatFloat(math.Round(n), 'f', 0, 64) + " " + units[unit]
	}
	return strconv.FormatFloat(n, 'f', 1, 64) + " " + units[unit]
}
