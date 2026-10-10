package database

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// columns extracts column/value pairs from Args, a map or a struct (or pointer to one).
// Struct fields map to their `db` tag or snake_cased name; `db:"-"` is skipped.
// With skipZero, zero-valued struct fields are left out (maps are taken as is).
func columns(data any, skipZero bool) ([]string, []any, error) {
	if args, ok := data.(Args); ok {
		data = map[string]any(args)
	}
	if m, ok := data.(map[string]any); ok {
		cols := slices.Sorted(maps.Keys(m))
		vals := make([]any, len(cols))
		for i, col := range cols {
			vals[i] = m[col]
		}
		return cols, vals, nil
	}

	v := reflect.ValueOf(data)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, nil, fmt.Errorf("database: expected struct or map, got %T", data)
	}

	var cols []string
	var vals []any
	collect(v, skipZero, &cols, &vals)
	return cols, vals, nil
}

func collect(v reflect.Value, skipZero bool, cols *[]string, vals *[]any) {
	for i := range v.NumField() {
		field, value := v.Type().Field(i), v.Field(i)

		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			collect(value, skipZero, cols, vals)
			continue
		}
		if !field.IsExported() || (skipZero && value.IsZero()) {
			continue
		}

		col := field.Tag.Get("db")
		if col == "-" {
			continue
		}
		if col == "" {
			col = snakeCase(field.Name)
		}
		*cols = append(*cols, col)
		*vals = append(*vals, value.Interface())
	}
}

// snakeCase converts Go names: ID -> id, UserID -> user_id, HTTPCode -> http_code.
func snakeCase(name string) string {
	runes := []rune(name)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func isZero(v any) bool {
	return v == nil || reflect.ValueOf(v).IsZero()
}
