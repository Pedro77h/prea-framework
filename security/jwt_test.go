package security

import (
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestJwt(t *testing.T) {
	SignKey = nil
	if _, err := (Jwt[string]{}).CreateToken("x", jwt.RegisteredClaims{}); err == nil {
		t.Fatal("empty key must not sign")
	}

	SignKey = []byte(strings.Repeat("k", MinKeySize))
	token, err := Jwt[string]{}.CreateToken("x", jwt.RegisteredClaims{})
	if err != nil {
		t.Fatal(err)
	}
	if claims, err := (Jwt[string]{}).DecodeToken(token); err != nil || claims.Payload != "x" {
		t.Fatalf("decode: %+v %v", claims, err)
	}

	hs512, _ := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.MapClaims{}).SignedString(SignKey)
	if _, err := (Jwt[string]{}).DecodeToken(hs512); err == nil {
		t.Fatal("only HS256 is accepted")
	}
}
