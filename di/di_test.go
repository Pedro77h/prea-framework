package di

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
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
	users := &Module{Name: "users", Providers: []any{Repo{}, Export(Service{Manual: manual})}}
	other := &Module{Name: "other", Imports: []Declarer{users}, Providers: []any{Consumer{}}}

	c := New(db)
	if err := c.Load(other, users); err != nil {
		t.Fatal(err)
	}

	svc := c.Providers(users)[1].(*Service)
	if svc.Repo == nil || svc.Repo.DB != db || svc.G != svc.Repo {
		t.Fatalf("bad injection: %+v", svc)
	}
	if svc.Repo != c.Providers(users)[0] {
		t.Fatal("injection must use the container's own instance")
	}
	if svc.Manual != manual || svc.Untagged != nil {
		t.Fatal("must keep values set in the declaration and skip untagged fields")
	}
	if got := c.Providers(other)[0].(*Consumer).Svc; got != svc {
		t.Fatal("import must reuse the same singleton")
	}
	if mods := c.Modules(); len(mods) != 2 || mods[0] != users || mods[1] != other {
		t.Fatalf("modules must load once, imports first: %v", mods)
	}
}

// Modules are package-level declarations: every container must build its own
// components from them, and leave the declaration untouched.
func TestIsolation(t *testing.T) {
	template := Service{}
	users := &Module{Name: "users", Providers: []any{Repo{}, Export(template)}}
	other := &Module{Name: "other", Imports: []Declarer{users}, Providers: []any{Consumer{}}}

	containers := make([]*Container, 8)
	var wg sync.WaitGroup
	for i := range containers {
		wg.Go(func() {
			c := New(&DB{fmt.Sprint(i)})
			if err := c.Load(other); err != nil {
				t.Error(err)
			}
			containers[i] = c
		})
	}
	wg.Wait()

	seen := map[*Service]bool{}
	for i, c := range containers {
		svc := c.Providers(other)[0].(*Consumer).Svc
		if svc.Repo.DB.Name != fmt.Sprint(i) {
			t.Errorf("container %d got the database of container %s", i, svc.Repo.DB.Name)
		}
		if seen[svc] {
			t.Errorf("container %d shares a component with another container", i)
		}
		seen[svc] = true
	}
	if template.Repo != nil || users.Providers[0].(Repo).DB != nil {
		t.Fatal("the declaration must never be modified")
	}
}

func TestErrors(t *testing.T) {
	notExported := &Module{Name: "a", Providers: []any{Repo{}}}
	cyc := &Module{Name: "c"}
	cyc.Imports = []Declarer{&Module{Name: "d", Imports: []Declarer{cyc}}}

	for want, m := range map[string]*Module{
		"no provider for *di.Consumer": {Name: "x", Providers: []any{struct {
			C *Consumer `inject:""`
		}{}}},
		"no provider for *di.Service":                 {Name: "b", Imports: []Declarer{notExported}, Providers: []any{Consumer{}}},
		"*di.DB must be declared as a value, di.DB{}": {Name: "p", Providers: []any{new(DB)}},
		"*di.DB must be declared as a value":          {Name: "e", Providers: []any{Export(&DB{})}},
		"provider string must be a struct value":      {Name: "y", Providers: []any{"db"}},
		"provider <nil> must be a struct value":       {Name: "n", Providers: []any{nil}},
		"duplicate provider":                          {Name: "z", Providers: []any{DB{}, DB{}}},
		"must be exported to be injected": {Name: "u", Providers: []any{struct {
			x *DB `inject:""`
		}{}}},
		"2 providers for di.Greeter": {Name: "g", Providers: []any{Repo{}, struct{ *Repo }{}, struct {
			G Greeter `inject:""`
		}{}}},
		"import cycle": cyc,
	} {
		err := New(&DB{}).Load(m)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}

func TestBareTag(t *testing.T) {
	// built at runtime: go vet rejects the bare tag in source code
	bare := reflect.StructOf([]reflect.StructField{{Name: "DB", Type: reflect.TypeFor[*DB](), Tag: `json:"-" inject`}})
	err := New(&DB{}).Load(&Module{Name: "b", Providers: []any{reflect.New(bare).Elem().Interface()}})
	if err == nil || !strings.Contains(err.Error(), `.DB: use the tag inject:""`) {
		t.Fatalf("a bare inject must fail with the fix: %v", err)
	}
}
