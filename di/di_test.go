package di

import (
	"reflect"
	"strings"
	"testing"
)

type DB struct{ Name string }

type Greeter interface{ Greet() string }

type Repo struct {
	DB *DB `inject:""`
}

func (*Repo) Greet() string { return "hi" }

type Base struct {
	Repo *Repo `inject:""`
}

type Service struct {
	Base
	G        Greeter `json:"-" inject:""`
	Manual   *Repo   `inject:""`
	Untagged *Repo
}

type Consumer struct {
	Svc *Service `inject:""`
}

func TestLoad(t *testing.T) {
	db := &DB{"pg"}
	manual := &Repo{}
	svc := &Service{Manual: manual}
	users := &Module{Name: "users", Providers: []any{new(Repo), Export(svc)}}
	other := &Module{Name: "other", Imports: []Declarer{users}, Providers: []any{new(Consumer)}}

	c := New(db)
	if err := c.Load(other, users); err != nil {
		t.Fatal(err)
	}

	if svc.Repo == nil || svc.Repo.DB != db || svc.G != svc.Repo {
		t.Fatalf("bad injection: %+v", svc)
	}
	if svc.Manual != manual || svc.Untagged != nil {
		t.Fatal("must keep manual values and skip untagged fields")
	}
	if got := c.Providers(other)[0].(*Consumer).Svc; got != svc {
		t.Fatal("import must reuse the same singleton")
	}
	if mods := c.Modules(); len(mods) != 2 || mods[0] != users || mods[1] != other {
		t.Fatalf("modules must load once, imports first: %v", mods)
	}
}

func TestErrors(t *testing.T) {
	notExported := &Module{Name: "a", Providers: []any{new(Repo)}}
	cyc := &Module{Name: "c"}
	cyc.Imports = []Declarer{&Module{Name: "d", Imports: []Declarer{cyc}}}

	for want, m := range map[string]*Module{
		"no provider for *di.Consumer": {Name: "x", Providers: []any{new(struct {
			C *Consumer `inject:""`
		})}},
		"no provider for *di.Service": {Name: "b", Imports: []Declarer{notExported}, Providers: []any{new(Consumer)}},
		"pointer to struct":           {Name: "y", Providers: []any{DB{}}},
		"duplicate provider":          {Name: "z", Providers: []any{new(DB), new(DB)}},
		"must be exported to be injected": {Name: "u", Providers: []any{new(struct {
			x *DB `inject:""`
		})}},
		"2 providers implement di.Greeter": {Name: "g", Providers: []any{new(Repo), new(struct{ *Repo }), new(struct {
			G Greeter `inject:""`
		})}},
		"import cycle": cyc,
	} {
		err := New(&DB{}).Load(m)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}

func TestBareTag(t *testing.T) {
	for tag, want := range map[reflect.StructTag]bool{
		`inject`: true, `json:"-" inject`: true, `inject:""`: true, `json:"inject"`: false, `injected`: false,
	} {
		if wantsInjection(reflect.StructField{Tag: tag}) != want {
			t.Errorf("%s: want %v", tag, want)
		}
	}
}
