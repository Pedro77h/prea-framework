package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLevels(t *testing.T) {
	var buf bytes.Buffer
	level, err := ParseLevel("success")
	if err != nil {
		t.Fatal(err)
	}
	Log = New(&buf, level, false)

	Debug("d")
	Info("i")
	Success("s", "id", 1)
	Error("e")

	out := buf.String()
	if strings.Contains(out, "msg=d") || strings.Contains(out, "msg=i") {
		t.Fatalf("below the level must be dropped:\n%s", out)
	}
	if !strings.Contains(out, "level=SUCCESS msg=s id=1") || !strings.Contains(out, "level=ERROR msg=e") {
		t.Fatalf("missing lines:\n%s", out)
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Fatal("invalid level must fail")
	}
}
