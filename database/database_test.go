package database

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"prea/di"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSnakeCase(t *testing.T) {
	for in, want := range map[string]string{
		"ID": "id", "Name": "name", "UserID": "user_id", "LastLogin": "last_login",
		"HTTPCode": "http_code", "Address2Line": "address2_line",
	} {
		if got := snakeCase(in); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestColumns(t *testing.T) {
	type Base struct{ OwnerID int }
	data := struct {
		Base
		Name    string
		Note    string `db:"comment"`
		Skip    string `db:"-"`
		private string
	}{Base{7}, "a", "", "x", "y"}

	cols, vals, _ := columns(&data, false)
	if !reflect.DeepEqual(cols, []string{"owner_id", "name", "comment"}) || !reflect.DeepEqual(vals, []any{7, "a", ""}) {
		t.Fatalf("all: %v %v", cols, vals)
	}
	if cols, _, _ := columns(data, true); !reflect.DeepEqual(cols, []string{"owner_id", "name"}) {
		t.Fatalf("skip zero: %v", cols)
	}
	if cols, vals, _ := columns(Args{"b": 0, "a": nil}, true); !reflect.DeepEqual(cols, []string{"a", "b"}) || !reflect.DeepEqual(vals, []any{nil, 0}) {
		t.Fatalf("args keep zero values, sorted: %v %v", cols, vals)
	}
	if _, _, err := columns(42, false); err == nil {
		t.Fatal("non struct must fail")
	}
}

type widget struct {
	ID       string     `json:"id" pk:"" generated:"uuid"`
	Name     string     `json:"name" unique:""`
	OwnerID  *int       `json:"owner_id"`
	LastSeen *time.Time `json:"last_seen"`
	Active   bool       `json:"active" default:"true"`
}

func (widget) Table() string { return "widgets" }

// testDB connects to PREA_TEST_DATABASE_URL and creates the tables of
// models, or skips the test when it is not set:
//
//	PREA_TEST_DATABASE_URL=postgres://... go test ./database
func testDB(t *testing.T, models ...any) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("PREA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PREA_TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, m := range models {
		reflect.ValueOf(m).Elem().FieldByName("DB").Set(reflect.ValueOf(pool))
	}
	if _, err := SyncSchema(t.Context(), SchemaCreate, models); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestRepository(t *testing.T) {
	ctx := context.Background()
	repo := &BaseRepository[widget]{}
	pool := testDB(t, repo)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	owner := 1

	a, err := repo.Create(ctx, struct {
		Name    string
		OwnerID *int
	}{"a", &owner})
	must(err)
	if a.ID == "" || a.Name != "a" || *a.OwnerID != 1 || !a.Active {
		t.Fatalf("create: %+v", a)
	}
	_, err = repo.Create(ctx, widget{Name: "b"}) // zero ID is left to the default
	must(err)

	if _, err := repo.Create(ctx, Args{"name": "a"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := repo.GetByID(ctx, "not-a-uuid"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing id: %v", err)
	}
	if got, err := repo.GetByID(ctx, a.ID); err != nil || got.Name != "a" {
		t.Fatalf("get: %+v %v", got, err)
	}

	if all, err := repo.GetAll(ctx); err != nil || len(all) != 2 {
		t.Fatalf("all: %v %v", all, err)
	}
	if ws, err := repo.Where(ctx, Args{"owner_id": nil}); err != nil || len(ws) != 1 || ws[0].Name != "b" {
		t.Fatalf("where is null: %v %v", ws, err)
	}
	if w, err := repo.First(ctx, Args{"owner_id": 1, "active": true}); err != nil || w.ID != a.ID {
		t.Fatalf("first: %+v %v", w, err)
	}
	if _, err := repo.Where(ctx, Args{`name" = 'a'; drop table widgets; --`: "x"}); err == nil {
		t.Fatal("column names must be quoted")
	}
	if _, err := repo.Where(ctx, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty filter: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	u, err := repo.Update(ctx, a.ID, widget{Name: "a2", LastSeen: &now})
	must(err)
	if u.Name != "a2" || !u.LastSeen.Equal(now) || !u.Active || *u.OwnerID != 1 {
		t.Fatalf("update skips zero fields: %+v", u)
	}
	if u, err := repo.Update(ctx, a.ID, Args{"active": false}); err != nil || u.Active {
		t.Fatalf("update with Args sets zero values: %+v %v", u, err)
	}
	if _, err := repo.Update(ctx, a.ID, widget{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty update: %v", err)
	}

	type summary struct{ Name string }
	names, err := repo.Prepare("select name from widgets where name like @prefix order by name").Bind("prefix", "a%").All(ctx)
	must(err)
	if len(names) != 1 || names[0].Name != "a2" {
		t.Fatalf("prepare: %+v", names)
	}
	if s, err := Prepare[summary](pool, "select name from widgets where id = @id").Bind("id", a.ID).One(ctx); err != nil || s.Name != "a2" {
		t.Fatalf("prepare into other type: %+v %v", s, err)
	}
	if n, err := Prepare[int64](pool, "select count(*) from widgets").One(ctx); err != nil || n != 2 {
		t.Fatalf("scalar: %v %v", n, err)
	}

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := Prepare[widget](tx, "delete from widgets").Exec(ctx); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if n, _ := Prepare[int64](pool, "select count(*) from widgets").One(ctx); err == nil || n != 2 {
		t.Fatalf("transaction must roll back: %v, %d rows", err, n)
	}

	if n, err := repo.Count(ctx, nil); err != nil || n != 2 {
		t.Fatalf("count all: %v %v", n, err)
	}
	if n, err := repo.Count(ctx, Args{"owner_id": nil}); err != nil || n != 1 {
		t.Fatalf("count filter: %v %v", n, err)
	}
	if ok, err := repo.Exists(ctx, Args{"name": "a2"}); err != nil || !ok {
		t.Fatalf("exists: %v %v", ok, err)
	}
	if ok, err := repo.Exists(ctx, Args{"name": "zz"}); err != nil || ok {
		t.Fatalf("not exists: %v %v", ok, err)
	}

	zero := 0
	if u, err := repo.Update(ctx, a.ID, struct{ OwnerID *int }{&zero}); err != nil || *u.OwnerID != 0 {
		t.Fatalf("update: a non-nil pointer writes its zero value: %+v %v", u, err)
	}

	// JPA style: load, change, save; zero values are written too
	loaded, err := repo.GetByID(ctx, a.ID)
	must(err)
	loaded.Active, loaded.OwnerID, loaded.Name = true, nil, ""
	if s, err := repo.Save(ctx, loaded); err != nil || s.Name != "" || s.OwnerID != nil || !s.Active || s.ID != a.ID {
		t.Fatalf("save writes every column: %+v %v", s, err)
	}
	created, err := repo.Save(ctx, widget{Name: "c"})
	if err != nil || created.ID == "" || created.Active {
		t.Fatalf("save with a zero primary key inserts every column but the key (no column default): %+v %v", created, err)
	}
	must(repo.Delete(ctx, created.ID))
	if _, err := repo.Save(ctx, widget{ID: "00000000-0000-0000-0000-000000000000", Name: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("save of a missing row: %v", err)
	}

	must(repo.Delete(ctx, a.ID))
	if err := repo.Delete(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestTransactional(t *testing.T) {
	ctx := context.Background()
	repo := &BaseRepository[widget]{}
	pool := testDB(t, repo)
	svc := Transactional{DB: pool}
	names := func(ctx context.Context) string {
		ns, err := Prepare[string](pool, "select name from widgets order by name").All(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(ns, ",")
	}
	fail := errors.New("fail")

	err := svc.Transaction(ctx, func(ctx context.Context) error {
		if _, err := repo.Create(ctx, widget{Name: "a"}); err != nil {
			return err
		}
		if names(ctx) != "a" || names(context.Background()) != "" {
			t.Error("the row must be visible only inside the transaction")
		}
		return nil
	})
	if err != nil || names(ctx) != "a" {
		t.Fatalf("commit: %v %q", err, names(ctx))
	}

	err = svc.Transaction(ctx, func(ctx context.Context) error {
		repo.Create(ctx, widget{Name: "b"})
		return fail
	})
	if !errors.Is(err, fail) || names(ctx) != "a" {
		t.Fatalf("an error rolls back: %v %q", err, names(ctx))
	}

	err = svc.Transaction(ctx, func(ctx context.Context) error {
		repo.Create(ctx, widget{Name: "c"})
		inner := svc.Transaction(ctx, func(ctx context.Context) error {
			repo.Create(ctx, widget{Name: "d"})
			_, err := repo.Create(ctx, widget{Name: "a"}) // duplicate
			return err
		})
		if !errors.Is(inner, ErrConflict) {
			t.Errorf("inner error must be translated: %v", inner)
		}
		return nil // the outer one decides to go on
	})
	if err != nil || names(ctx) != "a,c" {
		t.Fatalf("nested: a savepoint rolls back only the inner work: %v %q", err, names(ctx))
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic must propagate")
			}
		}()
		svc.Transaction(ctx, func(ctx context.Context) error {
			repo.Create(ctx, widget{Name: "e"})
			panic("boom")
		})
	}()
	if names(ctx) != "a,c" {
		t.Fatalf("a panic rolls back: %q", names(ctx))
	}
}

func TestTransactionalInjection(t *testing.T) {
	type service struct{ Transactional }
	pool := &pgxpool.Pool{}
	m := &di.Module{Name: "s", Providers: []any{service{}}}
	c := di.New(pool)
	if err := c.Load(m); err != nil {
		t.Fatal(err)
	}
	if got := c.Providers(m)[0].(*service).DB; got != pool {
		t.Fatalf("the embedded Transactional must get the pool: %v", got)
	}
}

func TestRepositoryKeys(t *testing.T) {
	ctx := t.Context()
	teams, members := &BaseRepository[team]{}, &BaseRepository[membership]{}
	testDB(t, teams, members)

	// auto_increment
	a, err := teams.Create(ctx, team{Name: "a"})
	if err != nil || a.ID == 0 {
		t.Fatalf("auto_increment: %+v %v", a, err)
	}
	b, err := teams.Save(ctx, team{Name: "b"})
	if err != nil || b.ID != a.ID+1 {
		t.Fatalf("save with a zero generated key inserts: %+v %v", b, err)
	}
	if _, err := teams.Save(ctx, team{ID: 999, Name: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("save with a missing generated key: %v", err)
	}

	// composite natural key: Save is an upsert, the id is Args
	key := Args{"team_id": int64(1), "user_id": int64(2)}
	m, err := members.Save(ctx, membership{TeamID: 1, UserID: 2, Role: "member"})
	if err != nil || m.Role != "member" || m.Rev != 0 {
		t.Fatalf("upsert inserts: %+v %v", m, err)
	}
	m.Role = "admin"
	if m, err = members.Save(ctx, m); err != nil || m.Role != "admin" || m.Rev != 1 {
		t.Fatalf("upsert updates and increments the version: %+v %v", m, err)
	}
	if got, err := members.GetByID(ctx, key); err != nil || got != m {
		t.Fatalf("get by composite key: %+v %v", got, err)
	}
	if _, err := members.GetByID(ctx, int64(1)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a composite key needs Args: %v", err)
	}
	if u, err := members.Update(ctx, key, struct{ Role string }{"owner"}); err != nil || u.Rev != 2 {
		t.Fatalf("update increments the version: %+v %v", u, err)
	}

	// optimistic lock: m is stale now (rev 1, the row has 2)
	m.Role = "stale"
	if _, err := members.Save(ctx, m); !errors.Is(err, ErrConflict) {
		t.Fatalf("a stale upsert: %v", err)
	}
	if err := members.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
}

type versioned struct {
	ID   string `pk:"" generated:"uuid"`
	Name string
	Rev  int32 `version:""`
}

func (versioned) Table() string { return "versioned" }

func TestOptimisticLock(t *testing.T) {
	ctx := t.Context()
	repo := &BaseRepository[versioned]{}
	testDB(t, repo)

	v, err := repo.Create(ctx, versioned{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	first, second := v, v // two requests load the same row
	first.Name, second.Name = "first", "second"
	if saved, err := repo.Save(ctx, first); err != nil || saved.Rev != 1 {
		t.Fatalf("first save: %+v %v", saved, err)
	}
	if _, err := repo.Save(ctx, second); !errors.Is(err, ErrConflict) {
		t.Fatalf("the second save must conflict: %v", err)
	}
	if got, _ := repo.GetByID(ctx, v.ID); got.Name != "first" {
		t.Fatalf("the conflicting save must not write: %+v", got)
	}
	if _, err := repo.Save(ctx, versioned{ID: "00000000-0000-0000-0000-000000000000", Name: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing row is not a conflict: %v", err)
	}
}

type gadget struct {
	ID    int64
	Name  string
	Color *string `index:""`
	Size  int16   `default:"1"`
}

func (gadget) Table() string { return "gadgets" }

type drift struct {
	ID    int64
	Name  string
	Score int64
	Count int64
	Note  *string
}

func (drift) Table() string { return "drift" }

type absent struct{ ID int64 }

func (absent) Table() string { return "absent" }

func TestSyncSchemaModes(t *testing.T) {
	ctx := t.Context()
	pool := testDB(t)
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	tableExists := func(name string) bool {
		var ok bool
		if err := pool.QueryRow(ctx, "select to_regclass($1) is not null", name).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	gadgets := &BaseRepository[gadget]{DB: pool}

	// update adds what is missing to a table with rows, and keeps them
	exec(`drop table if exists gadgets; create table gadgets (id int8 primary key, name text not null);
		insert into gadgets values (1, 'a')`)
	if _, err := SyncSchema(ctx, SchemaUpdate, []any{gadgets}); err != nil {
		t.Fatal(err)
	}
	if g, err := gadgets.GetByID(ctx, 1); err != nil || g.Name != "a" || g.Size != 1 || g.Color != nil {
		t.Fatalf("update: %+v %v", g, err)
	}
	if _, err := SyncSchema(ctx, SchemaUpdate, []any{gadgets}); err != nil {
		t.Fatalf("update is idempotent: %v", err)
	}
	if _, err := SyncSchema(ctx, SchemaValidate, []any{gadgets}); err != nil {
		t.Fatalf("validate after update: %v", err)
	}

	// validate reports every difference at once
	exec(`drop table if exists drift; drop table if exists absent;
		create table drift (id int8 primary key, name text, score int4 not null, note text not null)`)
	_, err := SyncSchema(ctx, SchemaValidate, []any{&BaseRepository[drift]{DB: pool}, &BaseRepository[absent]{DB: pool}})
	for _, want := range []string{
		"drift.name is nullable: make the field a pointer",
		"drift.score is int4, the model wants int8",
		"drift.count is missing",
		"drift.note is not null: make the field a value",
		"table absent is missing",
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("validate: want %q in %v", want, err)
		}
	}

	// create-drop: created now, dropped by drop
	drop, err := SyncSchema(ctx, SchemaCreateDrop, []any{gadgets})
	if err != nil || !tableExists("gadgets") {
		t.Fatalf("create-drop: %v", err)
	}
	if n, _ := gadgets.Count(ctx, nil); n != 0 {
		t.Fatalf("create starts from an empty table: %d rows", n)
	}
	if err := drop(ctx); err != nil || tableExists("gadgets") {
		t.Fatalf("drop: %v", err)
	}
}
