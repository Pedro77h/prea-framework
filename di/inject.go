package di

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func injectFields(v reflect.Value, scopes [][]component, deps *[]any) error {
	for i := range v.NumField() {
		field, value := v.Type().Field(i), v.Field(i)

		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if err := injectFields(value, scopes, deps); err != nil {
				return err
			}
			continue
		}
		name := v.Type().Name() + "." + field.Name
		if key, ok := field.Tag.Lookup("value"); ok {
			if err := fillValue(value, key, scopes); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			continue
		}
		tag, tagged := field.Tag.Lookup("inject")
		if !tagged && slices.Contains(strings.Fields(string(field.Tag)), "inject") {
			return fmt.Errorf(`%s: use the tag inject:"" (a bare inject is not a valid Go struct tag)`, name)
		}
		if !tagged || !value.IsZero() {
			continue
		}
		if !value.CanSet() {
			return fmt.Errorf("%s must be exported to be injected", name)
		}
		dep, err := resolve(field.Type, tag, scopes)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		value.Set(reflect.ValueOf(dep))
		*deps = append(*deps, dep)
	}
	return nil
}

func fillValue(v reflect.Value, key string, scopes [][]component) error {
	if !v.IsZero() {
		return nil
	}
	if key == "" {
		return fmt.Errorf(`value tag needs a key, e.g. value:"database.url"`)
	}
	if !v.CanSet() {
		return fmt.Errorf("must be exported to receive a value")
	}
	src, err := resolve(reflect.TypeFor[Values](), "", scopes)
	if err != nil {
		return fmt.Errorf("value:%q needs a config (core.App.Config): %w", key, err)
	}
	return src.(Values).Value(key, v.Addr().Interface())
}

func resolve(t reflect.Type, name string, scopes [][]component) (any, error) {
	for _, scope := range scopes {
		dep, found, err := find(t, name, scope)
		if found || err != nil {
			return dep, err
		}
	}
	return nil, fmt.Errorf("no provider for %v%s", t, describe(name))
}

func find(t reflect.Type, name string, scope []component) (dep any, found bool, err error) {
	var exact, implementers []component
	for _, p := range scope {
		if p.value == nil || name != "" && p.name != name {
			continue
		}
		pt := reflect.TypeOf(p.value)
		switch {
		case pt == t:
			exact = append(exact, p)
		case t.Kind() == reflect.Interface && pt.Implements(t):
			implementers = append(implementers, p)
		}
	}

	candidates := exact
	if len(candidates) == 0 {
		candidates = implementers
	}
	if name == "" {
		if unnamed := slices.DeleteFunc(slices.Clone(candidates), func(p component) bool { return p.name != "" }); len(unnamed) > 0 {
			candidates = unnamed
		}
	}

	switch len(candidates) {
	case 0:
		return nil, false, nil
	case 1:
		return candidates[0].value, true, nil
	default:
		return nil, true, fmt.Errorf(`%d providers for %v: name them with di.Named and pick one with inject:"<name>"`, len(candidates), t)
	}
}

func describe(name string) string {
	if name == "" {
		return ""
	}
	return fmt.Sprintf(" named %q", name)
}
