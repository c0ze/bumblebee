package transform

import (
	"fmt"
	"strconv"
	"strings"
)

// Int returns p[key] coerced to int, or def. Handles int/int64/float64/string
// (YAML decodes numbers as int/float64; request overrides arrive as strings).
func (p Params) Int(key string, def int) int {
	v, ok := p[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i
		}
	}
	return def
}

// String returns p[key] as a string, or def.
func (p Params) String(key, def string) string {
	v, ok := p[key]
	if !ok {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// Bool returns p[key] coerced to bool, or def.
func (p Params) Bool(key string, def bool) bool {
	v, ok := p[key]
	if !ok {
		return def
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true" || b == "1"
	}
	return def
}
