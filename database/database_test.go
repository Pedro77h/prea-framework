package database

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"prea/common"
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
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	OwnerID  *int       `json:"owner_id"`
	LastSeen *time.Time `json:"last_seen"`
	Active   bool       `json:"active"`
}

func (widget) Table() string { return "widgets" }

// Runs against a real database: PREA_TEST_DATABASE_URL=postgres://... go test ./database
func TestRepository(t *testing.T) {
	url := os.Getenv("PREA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PREA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = pool.Exec(ctx, `drop table if exists widgets;
		create table widgets (
			id uuid primary key default gen_random_uuid(),
			name text not null unique,
			owner_id int,
			last_seen timestamptz,
			active bool not null default true
		)`)
	must(err)

	repo := &BaseRepository[widget]{DB: pool}
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

	if _, err := repo.Create(ctx, Args{"name": "a"}); !errors.Is(err, common.ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := repo.GetByID(ctx, "not-a-uuid"); !errors.Is(err, common.ErrInvalidInput) {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, common.ErrNotFound) {
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
	if _, err := repo.Where(ctx, nil); !errors.Is(err, common.ErrInvalidInput) {
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
	if _, err := repo.Update(ctx, a.ID, widget{}); !errors.Is(err, common.ErrInvalidInput) {
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

	must(repo.Delete(ctx, a.ID))
	if err := repo.Delete(ctx, a.ID); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

// Runs against a real database: PREA_TEST_DATABASE_URL=postgres://... go test ./database
func TestTransactional(t *testing.T) {
	url := os.Getenv("PREA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PREA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `drop table if exists widgets; create table widgets (
		id uuid primary key default gen_random_uuid(), name text not null unique,
		owner_id int, last_seen timestamptz, active bool not null default true)`); err != nil {
		t.Fatal(err)
	}

	repo := &BaseRepository[widget]{DB: pool}
	svc := Transactional{DB: pool}
	names := func(ctx context.Context) string {
		ns, err := Prepare[string](pool, "select name from widgets order by name").All(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(ns, ",")
	}
	fail := errors.New("fail")

	err = svc.Transaction(ctx, func(ctx context.Context) error {
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
		if !errors.Is(inner, common.ErrConflict) {
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
