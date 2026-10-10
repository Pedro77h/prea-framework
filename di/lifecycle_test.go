package di

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type Pool struct{ Name string }

type Reports struct {
	Main      *Pool `inject:""`
	Analytics *Pool `inject:"analytics"`
}

func TestNamed(t *testing.T) {
	main, analytics := &Pool{"main"}, &Pool{"analytics"}
	m := &Module{Name: "r", Providers: []any{Reports{}}}

	c := New(main, Named("analytics", analytics))
	if err := c.Load(m); err != nil {
		t.Fatal(err)
	}
	r := c.Providers(m)[0].(*Reports)
	if r.Main != main || r.Analytics != analytics {
		t.Fatalf(`inject:"" picks the unnamed one, inject:"analytics" the named one: %+v`, r)
	}

	clients := &Module{Name: "clients", Providers: []any{
		Named("a", DB{}), Export(Named("b", DB{Name: "b"})), Named("c", Export(DB{Name: "c"})),
	}}
	user := &Module{Name: "user", Imports: []Declarer{clients}, Providers: []any{struct {
		B *DB `inject:"b"`
		C *DB `inject:"c"`
	}{}}}
	c = New()
	if err := c.Load(user); err != nil {
		t.Fatal(err)
	}
	if got := c.Providers(user)[0].(*struct {
		B *DB `inject:"b"`
		C *DB `inject:"c"`
	}); got.B.Name != "b" || got.C.Name != "c" {
		t.Fatalf("Named and Export compose in any order: %+v", got)
	}

	only := New(Named("analytics", analytics))
	if err := only.Load(&Module{Name: "o", Providers: []any{struct {
		P *Pool `inject:""`
	}{}}}); err != nil {
		t.Fatalf("a single named provider still serves inject:\"\": %v", err)
	}

	for want, c := range map[string]*Container{
		"2 providers for *di.Pool":                   New(&Pool{}, &Pool{}),
		`no provider for *di.Pool named "analytics"`: New(&Pool{}),
	} {
		if err := c.Load(&Module{Name: "x", Providers: []any{Reports{}}}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
	if err := New().Load(&Module{Name: "d", Providers: []any{Named("a", DB{}), Named("a", DB{})}}); err == nil || !strings.Contains(err.Error(), `duplicate provider di.DB named "a"`) {
		t.Errorf("duplicate name: %v", err)
	}
}

var events []string

type step struct {
	name    string
	failure error
}

func (s *step) init(context.Context) error {
	events = append(events, "init "+s.name)
	return s.failure
}

func (s *step) close() { events = append(events, "close "+s.name) }

type Store struct{ step }

func (s *Store) Init(ctx context.Context) error { return s.init(ctx) }
func (s *Store) Close(context.Context) error    { s.close(); return nil }

type Cache struct {
	step
	Store *Store `inject:""`
}

func (c *Cache) Init(ctx context.Context) error { return c.init(ctx) }
func (c *Cache) Close() error                   { c.close(); return nil }

type API struct {
	step
	Cache *Cache `inject:""`
}

func (a *API) Init(ctx context.Context) error { return a.init(ctx) }
func (a *API) Close(context.Context) error    { a.close(); return nil }

type Global struct{}

func (*Global) Init(context.Context) error { events = append(events, "init global"); return nil }

func TestLifecycle(t *testing.T) {
	ctx := context.Background()
	// API is declared first and Store lives in an imported module: Init must
	// still follow the dependencies, and Close the reverse.
	store := &Module{Name: "store", Providers: []any{Export(Store{step{name: "store"}})}}
	app := &Module{Name: "app", Imports: []Declarer{store}, Providers: []any{
		API{step: step{name: "api"}}, Cache{step: step{name: "cache"}},
	}}

	events = nil
	c := New(&Global{})
	if err := c.Load(app); err != nil {
		t.Fatal(err)
	}
	if err := c.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	want := "init store,init cache,init api,close api,close cache,close store"
	if got := strings.Join(events, ","); got != want {
		t.Fatalf("got  %s\nwant %s (globals are not managed, Close runs once)", got, want)
	}

	events = nil
	boom := errors.New("boom")
	failing := &Module{Name: "f", Imports: []Declarer{store}, Providers: []any{
		API{step: step{name: "api"}}, Cache{step: step{name: "cache", failure: boom}},
	}}
	c = New()
	if err := c.Load(failing); err != nil {
		t.Fatal(err)
	}
	if err := c.Init(ctx); !errors.Is(err, boom) || !strings.Contains(err.Error(), "*di.Cache.Init") {
		t.Fatalf("init error: %v", err)
	}
	if got := strings.Join(events, ","); got != "init store,init cache,close store" {
		t.Fatalf("a failed Init closes what already started: %s", got)
	}
}
