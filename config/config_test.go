package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("application.yaml", `
server:
  port: 8000
  timeout: 30s
  debug: ${DEBUG}
mail:
  addr: ${MAIL_HOST}:25
  from: a@x.com
  password: ${MAIL_PASSWORD}
`)
	write("application-dev.yaml", "server:\n  port: 9000\nmail:\n  from: dev@x.com\n")
	t.Chdir(dir) // no .env here
	t.Setenv(ProfileEnv, "dev")
	t.Setenv("DEBUG", "true")
	t.Setenv("MAIL_HOST", "0123")
	t.Setenv("PREA_SERVER_PORT", "1") // not referenced: ignored

	c, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Profile() != "dev" {
		t.Fatalf("profile %q, want dev", c.Profile())
	}

	var port int
	var timeout time.Duration
	var debug bool
	var mail struct {
		Addr     string  `yaml:"addr"`
		From     string  `yaml:"from"`
		Password *string `yaml:"password"`
	}
	for key, dst := range map[string]any{"server.port": &port, "server.timeout": &timeout, "server.debug": &debug, "mail": &mail} {
		if err := c.Value(key, dst); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if port != 9000 || timeout != 30*time.Second || !debug {
		t.Fatalf("port %d timeout %v debug %v", port, timeout, debug)
	}
	// profile merged into the block; env kept as written; unset ${VAR} is nil
	if mail.From != "dev@x.com" || mail.Addr != "0123:25" || mail.Password != nil {
		t.Fatalf("mail %+v", mail)
	}

	missing := "kept"
	if err := c.Value("server.missing", &missing); err != nil || missing != "kept" {
		t.Fatalf("a missing key must leave the value: %q %v", missing, err)
	}
	if err := c.Value("mail.password", &missing); err != nil || missing != "kept" {
		t.Fatalf("an unset ${VAR} must leave the value: %q %v", missing, err)
	}
}
