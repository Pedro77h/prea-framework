package security

import (
	"errors"
	"fmt"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"prea/common"
)

const (
	DefaultAlg string = "HS256"
	JwtEnv     string = "JWTKEY"
	MinKeySize        = 32
)

var (
	ErrInvalidToken       = errors.New("invalid token")             // bad signature, expired, wrong algorithm; core.Error answers 401
	ErrInvalidHash        = errors.New("invalid hash")              // stored password hash is malformed
	ErrInvalidHashVersion = errors.New("invalid algorithm version") // stored hash from another argon2 version
)

var SignKey []byte

func init() {
	SignKey = []byte(common.GetEnv(JwtEnv))
	// Fail at startup; tests set SignKey themselves.
	if _, err := signKey(); err != nil && !testing.Testing() {
		panic(err)
	}
}

func signKey() ([]byte, error) {
	if len(SignKey) < MinKeySize {
		return nil, fmt.Errorf("security: %s must be set with at least %d bytes (got %d)", JwtEnv, MinKeySize, len(SignKey))
	}
	return SignKey, nil
}

type Jwt[M any] struct{}

type DataClaims[M any] struct {
	Payload M `json:"payload"`
	jwt.RegisteredClaims
}

func (Jwt[M]) CreateToken(object M, claims jwt.RegisteredClaims) (string, error) {
	token := jwt.New(jwt.GetSigningMethod(DefaultAlg))

	token.Claims = &DataClaims[M]{
		object,
		claims,
	}

	key, err := signKey()
	if err != nil {
		return "", err
	}
	encoded, err := token.SignedString(key)
	if err != nil {
		return "", err
	}

	return encoded, nil
}

func (Jwt[M]) DecodeToken(token string) (DataClaims[M], error) {
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		return signKey()
	}

	parsed, err := jwt.ParseWithClaims(token, &DataClaims[M]{}, keyFunc, jwt.WithValidMethods([]string{DefaultAlg}))
	if err != nil {
		return DataClaims[M]{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	return *parsed.Claims.(*DataClaims[M]), nil
}
