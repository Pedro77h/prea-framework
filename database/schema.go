package database

import (
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// table is a model's schema, read from its struct tags:
//
//	type User struct {
//		ID      string  `pk:"" generated:"uuid"`
//		Email   string  `size:"255" unique:""`
//		TeamID  *string `fk:"teams" ondelete:"cascade" index:""`
//		Profile int16   `default:"1"`
//		Version int64   `version:""`
//	}
//
// The column name comes from db:"name" (or the snake_cased field name) and
// nullability from the Go type: a pointer is null, a value is not null.
type table struct {
	name    string // Model.Table(), unquoted
	typ     reflect.Type
	columns []column
}

type column struct {
	name      string
	sqlType   string // Postgres udt_name: text, varchar, int8, uuid...
	size      int    // varchar(size)
	nullable  bool
	pk        bool
	generated string // "", "auto_increment" or "uuid"
	def       string // SQL literal
	unique    string // group: columns with the same group share a constraint
	index     string // group, like unique
	fk        string // referenced table
	onDelete  string
	version   bool
}

func (t *table) pk() []column {
	return slices.DeleteFunc(slices.Clone(t.columns), func(c column) bool { return !c.pk })
}

func (t *table) version() (column, bool) {
	i := slices.IndexFunc(t.columns, func(c column) bool { return c.version })
	if i < 0 {
		return column{}, false
	}
	return t.columns[i], true
}

func (t *table) quoted() string {
	return quoteTable(t.name)
}

type schemaResult struct {
	t   *table
	err error
}

var schemas sync.Map // reflect.Type -> schemaResult

// schemaOf reads the schema of a model type once; later calls hit the cache.
func schemaOf(typ reflect.Type) (*table, error) {
	if r, ok := schemas.Load(typ); ok {
		return r.(schemaResult).t, r.(schemaResult).err
	}
	t, err := parseSchema(typ)
	schemas.Store(typ, schemaResult{t, err})
	return t, err
}

func parseSchema(typ reflect.Type) (*table, error) {
	model, ok := reflect.Zero(typ).Interface().(Model)
	if typ.Kind() != reflect.Struct || !ok {
		return nil, fmt.Errorf("database: %v must be a struct with a Table() string method", typ)
	}
	t := &table{name: model.Table(), typ: typ}

	var errs []error
	parseFields(typ, typ.String(), t, &errs)

	seen := map[string]bool{}
	for _, c := range t.columns {
		if seen[c.name] {
			errs = append(errs, fmt.Errorf("database: %v: column %q is declared twice", typ, c.name))
		}
		seen[c.name] = true
	}
	if n := len(slices.DeleteFunc(slices.Clone(t.columns), func(c column) bool { return !c.version })); n > 1 {
		errs = append(errs, fmt.Errorf("database: %v: %d version fields, only one is allowed", typ, n))
	}
	if len(t.pk()) == 0 {
		i := slices.IndexFunc(t.columns, func(c column) bool { return c.name == "id" })
		if i < 0 {
			errs = append(errs, fmt.Errorf(`database: %v: no primary key: tag a field with pk:"" or name it ID`, typ))
		} else if t.columns[i].nullable {
			errs = append(errs, fmt.Errorf("database: %v: primary key id can't be a pointer", typ))
		} else {
			t.columns[i].pk = true
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return t, nil
}

func parseFields(typ reflect.Type, path string, t *table, errs *[]error) {
	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			parseFields(field.Type, path, t, errs)
			continue
		}
		name := field.Tag.Get("db")
		if !field.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = snakeCase(field.Name)
		}
		c, err := parseColumn(field, name)
		if err != nil {
			*errs = append(*errs, fmt.Errorf("database: %s.%s: %w", path, field.Name, err))
			continue
		}
		t.columns = append(t.columns, c)
	}
}

func parseColumn(field reflect.StructField, name string) (column, error) {
	tag := field.Tag
	goType := field.Type
	c := column{name: name}
	if goType.Kind() == reflect.Pointer {
		goType, c.nullable = goType.Elem(), true
	}
	kind := goType.Kind()
	isInt := slices.Contains([]reflect.Kind{reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64}, kind)

	c.sqlType = inferType(goType)
	_, c.pk = tag.Lookup("pk")
	_, c.version = tag.Lookup("version")
	c.fk, c.onDelete = tag.Get("fk"), tag.Get("ondelete")
	if group, ok := tag.Lookup("unique"); ok {
		c.unique = cmp.Or(group, name)
	}
	if group, ok := tag.Lookup("index"); ok {
		c.index = cmp.Or(group, name)
	}

	if c.pk && c.nullable {
		return c, fmt.Errorf("primary key can't be a pointer")
	}
	if g, ok := tag.Lookup("generated"); ok {
		if !c.pk {
			return c, fmt.Errorf(`generated needs pk:""`)
		}
		if g == "auto" {
			switch {
			case isInt:
				g = "auto_increment"
			case kind == reflect.String:
				g = "uuid"
			default:
				return c, fmt.Errorf("generated:%q needs an integer or string field", g)
			}
		}
		switch {
		case g == "auto_increment" && isInt:
		case g == "uuid" && kind == reflect.String:
			c.sqlType = "uuid"
		case g == "auto_increment" || g == "uuid":
			return c, fmt.Errorf("generated:%q doesn't fit a %v field", g, goType)
		default:
			return c, fmt.Errorf("generated:%q is not auto_increment, uuid or auto", g)
		}
		c.generated = g
	}
	if s, ok := tag.Lookup("size"); ok {
		n, err := strconv.Atoi(s)
		if kind != reflect.String || err != nil || n <= 0 {
			return c, fmt.Errorf("size:%q needs a string field and a positive number", s)
		}
		c.sqlType, c.size = "varchar", n
	}
	if typ, ok := tag.Lookup("type"); ok {
		if typ == "" {
			return c, fmt.Errorf(`type needs a Postgres type, e.g. type:"jsonb"`)
		}
		c.sqlType = strings.ToLower(typ)
	}
	if c.sqlType == "" {
		return c, fmt.Errorf(`no column type for %v: set one with type:"..."`, goType)
	}
	if lit, ok := tag.Lookup("default"); ok {
		def, err := literal(goType, lit)
		if err != nil {
			return c, err
		}
		c.def = def
	}
	if c.version && (!isInt || c.nullable) {
		return c, fmt.Errorf("version needs an integer field")
	}
	if c.onDelete != "" && c.fk == "" {
		return c, fmt.Errorf(`ondelete needs fk:"<table>"`)
	}
	if !slices.Contains([]string{"", "cascade", "restrict", "set null"}, c.onDelete) {
		return c, fmt.Errorf("ondelete:%q is not cascade, restrict or set null", c.onDelete)
	}
	return c, nil
}

// inferType maps a Go type to its Postgres udt_name; "" when there's none.
func inferType(t reflect.Type) string {
	if t == timeType {
		return "timestamptz"
	}
	switch t.Kind() {
	case reflect.String:
		return "text"
	case reflect.Int, reflect.Int64:
		return "int8"
	case reflect.Int32:
		return "int4"
	case reflect.Int16, reflect.Int8:
		return "int2"
	case reflect.Bool:
		return "bool"
	case reflect.Float64:
		return "float8"
	case reflect.Float32:
		return "float4"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "bytea"
		}
	}
	return ""
}

// literal turns a default:"..." value into SQL: strings are quoted, numbers
// and bools must parse as the field's type.
func literal(t reflect.Type, s string) (string, error) {
	var err error
	out := s
	switch t.Kind() {
	case reflect.String:
		return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
	case reflect.Bool:
		var b bool
		b, err = strconv.ParseBool(s)
		out = strconv.FormatBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		_, err = strconv.ParseInt(s, 10, t.Bits())
	case reflect.Float32, reflect.Float64:
		_, err = strconv.ParseFloat(s, t.Bits())
	default:
		return "", fmt.Errorf("default supports string, number and bool fields, not %v", t)
	}
	if err != nil {
		return "", fmt.Errorf("default:%q is not a valid %v", s, t)
	}
	return out, nil
}
