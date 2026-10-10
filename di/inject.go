package di

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func injectFields(v reflect.Value, scopes [][]any) error {
	for i := range v.NumField() {
		field, value := v.Type().Field(i), v.Field(i)

		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if err := injectFields(value, scopes); err != nil {
				return err
			}
			continue
		}
		if !wantsInjection(field) || !value.IsZero() {
			continue
		}

		name := v.Type().Name() + "." + field.Name
		if !value.CanSet() {
			return fmt.Errorf("%s must be exported to be injected", name)
		}
		dep, err := resolve(field.Type, scopes)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		value.Set(reflect.ValueOf(dep))
	}
	return nil
}

func wantsInjection(field reflect.StructField) bool {
	_, tagged := field.Tag.Lookup("inject")
	return tagged || slices.Contains(strings.Fields(string(field.Tag)), "inject")
}

func resolve(t reflect.Type, scopes [][]any) (any, error) {
	for _, scope := range scopes {
		dep, found, err := find(t, scope)
		if found || err != nil {
			return dep, err
		}
	}
	return nil, fmt.Errorf("no provider for %v", t)
}

func find(t reflect.Type, scope []any) (dep any, found bool, err error) {
	var candidates []any
	for _, p := range scope {
		pt := reflect.TypeOf(p)
		if pt == t {
			return p, true, nil
		}
		if t.Kind() == reflect.Interface && pt.Implements(t) {
			candidates = append(candidates, p)
		}
	}

	switch len(candidates) {
	case 0:
		return nil, false, nil
	case 1:
		return candidates[0], true, nil
	default:
		return nil, true, fmt.Errorf("%d providers implement %v", len(candidates), t)
	}
}
