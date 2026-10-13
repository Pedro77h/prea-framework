package database

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

type tagged struct {
	Base
	Code      string     `pk:"" size:"12"`
	Region    int16      `pk:""`
	Email     string     `unique:""`
	First     string     `unique:"full_name"`
	Last      string     `unique:"full_name"`
	TeamID    *int64     `fk:"teams" ondelete:"set null" index:""`
	Profile   int16      `default:"1"`
	Title     string     `default:"it's"`
	Active    bool       `default:"TRUE"`
	Data      []byte     `type:"JSONB"`
	Seen      *time.Time `db:"last_seen"`
	Rev       int64      `version:""`
	Ignored   string     `db:"-"`
	unexposed string
}

type Base struct{ CreatedAt time.Time }

func (tagged) Table() string { return "app.tagged" }

func TestSchemaColumns(t *testing.T) {
	got, err := schemaOf(reflect.TypeFor[tagged]())
	if err != nil {
		t.Fatal(err)
	}
	want := []column{
		{name: "created_at", sqlType: "timestamptz"},
		{name: "code", sqlType: "varchar", size: 12, pk: true},
		{name: "region", sqlType: "int2", pk: true},
		{name: "email", sqlType: "text", unique: "email"},
		{name: "first", sqlType: "text", unique: "full_name"},
		{name: "last", sqlType: "text", unique: "full_name"},
		{name: "team_id", sqlType: "int8", nullable: true, fk: "teams", onDelete: "set null", index: "team_id"},
		{name: "profile", sqlType: "int2", def: "1"},
		{name: "title", sqlType: "text", def: "'it''s'"},
		{name: "active", sqlType: "bool", def: "true"},
		{name: "data", sqlType: "jsonb"},
		{name: "last_seen", sqlType: "timestamptz", nullable: true},
		{name: "rev", sqlType: "int8", version: true},
	}
	if got.name != "app.tagged" || !reflect.DeepEqual(got.columns, want) {
		t.Fatalf("got %s\n%+v\nwant\n%+v", got.name, got.columns, want)
	}
	if v, ok := got.version(); !ok || v.name != "rev" {
		t.Fatalf("version: %+v", v)
	}
	if pk := got.pk(); len(pk) != 2 || pk[0].name != "code" || pk[1].name != "region" {
		t.Fatalf("pk: %+v", pk)
	}
	if again, _ := schemaOf(reflect.TypeFor[tagged]()); again != got {
		t.Fatal("schema must be cached")
	}
}

type implicitID struct {
	ID   int64
	Name string
}

func (implicitID) Table() string { return "implicit" }

type autoInt struct {
	ID int32 `pk:"" generated:"auto"`
}

func (autoInt) Table() string { return "auto_int" }

type autoUUID struct {
	Key string `pk:"" generated:"auto"`
}

func (autoUUID) Table() string { return "auto_uuid" }

func TestSchemaPrimaryKey(t *testing.T) {
	for _, tc := range []struct {
		typ       reflect.Type
		name      string
		sqlType   string
		generated string
	}{
		{reflect.TypeFor[implicitID](), "id", "int8", ""},
		{reflect.TypeFor[autoInt](), "id", "int4", "auto_increment"},
		{reflect.TypeFor[autoUUID](), "key", "uuid", "uuid"},
		{reflect.TypeFor[widget](), "id", "uuid", "uuid"},
	} {
		s, err := schemaOf(tc.typ)
		if err != nil {
			t.Fatalf("%v: %v", tc.typ, err)
		}
		pk := s.pk()
		if len(pk) != 1 || pk[0].name != tc.name || pk[0].sqlType != tc.sqlType || pk[0].generated != tc.generated {
			t.Errorf("%v: pk %+v", tc.typ, pk)
		}
	}
}

// Each invalid model is its own type so the cache doesn't mix them up.
type (
	noKey      struct{ Name string }
	pointerKey struct{ ID *int64 }
	genNotPK   struct {
		ID  int64
		Seq int64 `generated:"auto_increment"`
	}
	genBadValue struct {
		ID int64 `pk:"" generated:"sequence"`
	}
	genUUIDOnInt struct {
		ID int64 `pk:"" generated:"uuid"`
	}
	genIncOnString struct {
		ID string `pk:"" generated:"auto_increment"`
	}
	genAutoOnBool struct {
		ID bool `pk:"" generated:"auto"`
	}
	sizeOnInt struct {
		ID int64 `size:"10"`
	}
	sizeNotNumber struct {
		ID   int64
		Name string `size:"big"`
	}
	emptyType struct {
		ID   int64
		Data string `type:""`
	}
	unknownType struct {
		ID   int64
		Tags []string
	}
	badIntDefault struct {
		ID    int64
		Count int16 `default:"99999"`
	}
	badBoolDefault struct {
		ID     int64
		Active bool `default:"yes"`
	}
	defaultOnTime struct {
		ID   int64
		When time.Time `default:"now"`
	}
	versionOnString struct {
		ID  int64
		Rev string `version:""`
	}
	twoVersions struct {
		ID int64
		A  int64 `version:""`
		B  int64 `version:""`
	}
	onDeleteNoFK struct {
		ID     int64
		TeamID int64 `ondelete:"cascade"`
	}
	badOnDelete struct {
		ID     int64
		TeamID int64 `fk:"teams" ondelete:"nothing"`
	}
	duplicate struct {
		ID    int64
		Name  string
		Other string `db:"name"`
	}
	notModel struct{ ID int64 }
)

func (noKey) Table() string           { return "t" }
func (pointerKey) Table() string      { return "t" }
func (genNotPK) Table() string        { return "t" }
func (genBadValue) Table() string     { return "t" }
func (genUUIDOnInt) Table() string    { return "t" }
func (genIncOnString) Table() string  { return "t" }
func (genAutoOnBool) Table() string   { return "t" }
func (sizeOnInt) Table() string       { return "t" }
func (sizeNotNumber) Table() string   { return "t" }
func (emptyType) Table() string       { return "t" }
func (unknownType) Table() string     { return "t" }
func (badIntDefault) Table() string   { return "t" }
func (badBoolDefault) Table() string  { return "t" }
func (defaultOnTime) Table() string   { return "t" }
func (versionOnString) Table() string { return "t" }
func (twoVersions) Table() string     { return "t" }
func (onDeleteNoFK) Table() string    { return "t" }
func (badOnDelete) Table() string     { return "t" }
func (duplicate) Table() string       { return "t" }

func TestSchemaErrors(t *testing.T) {
	for typ, want := range map[reflect.Type]string{
		reflect.TypeFor[noKey]():           "database.noKey: no primary key",
		reflect.TypeFor[pointerKey]():      "primary key id can't be a pointer",
		reflect.TypeFor[genNotPK]():        `database.genNotPK.Seq: generated needs pk:""`,
		reflect.TypeFor[genBadValue]():     `generated:"sequence" is not auto_increment, uuid or auto`,
		reflect.TypeFor[genUUIDOnInt]():    `generated:"uuid" doesn't fit a int64 field`,
		reflect.TypeFor[genIncOnString]():  `generated:"auto_increment" doesn't fit a string field`,
		reflect.TypeFor[genAutoOnBool]():   `generated:"auto" needs an integer or string field`,
		reflect.TypeFor[sizeOnInt]():       `size:"10" needs a string field`,
		reflect.TypeFor[sizeNotNumber]():   `size:"big" needs a string field and a positive number`,
		reflect.TypeFor[emptyType]():       "type needs a Postgres type",
		reflect.TypeFor[unknownType]():     "database.unknownType.Tags: no column type for []string",
		reflect.TypeFor[badIntDefault]():   `default:"99999" is not a valid int16`,
		reflect.TypeFor[badBoolDefault]():  `default:"yes" is not a valid bool`,
		reflect.TypeFor[defaultOnTime]():   "default supports string, number and bool fields, not time.Time",
		reflect.TypeFor[versionOnString](): "version needs an integer field",
		reflect.TypeFor[twoVersions]():     "2 version fields, only one is allowed",
		reflect.TypeFor[onDeleteNoFK]():    `ondelete needs fk:"<table>"`,
		reflect.TypeFor[badOnDelete]():     `ondelete:"nothing" is not cascade, restrict or set null`,
		reflect.TypeFor[duplicate]():       `column "name" is declared twice`,
		reflect.TypeFor[notModel]():        "must be a struct with a Table() string method",
	} {
		_, err := schemaOf(typ)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: got %v, want %q", typ, err, want)
		}
	}
}

func TestSchemaReportsEveryError(t *testing.T) {
	type fields struct {
		ID int64
		A  string `size:"x"`
		B  []int
	}
	var errs []error
	parseFields(reflect.TypeFor[fields](), "fields", &table{}, &errs)
	if len(errs) != 2 {
		t.Fatalf("every field error is reported: %v", errs)
	}
}
