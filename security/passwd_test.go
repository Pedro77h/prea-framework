package security_test

import (
	"prea/security"
	"testing"
)

func TestHashPassword(t *testing.T) {
	hs, err := security.HashPassword("Renan")
	if err != nil {
		t.Fatal(err)
	}

	cp, err := security.ComparePassword("Renan", hs)
	if err != nil {
		t.Fatal(err)
	}

	if !cp {
		t.Fatalf("expected true")
	}

	cp, err = security.ComparePassword("Naner", hs)
	if err != nil {
		t.Fatal(err)
	}

	if cp {
		t.Fatalf("expected false")
	}
}