package security

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

const (
	DefaultAlg string = "HS256"
	MinKeySize        = 32
)

var (
	ErrInvalidToken       = errors.New("invalid token")             // bad signature, expired, wrong algorithm; core.Error answers 401
	ErrInvalidHash        = errors.New("invalid hash")              // stored password hash is malformed
	ErrInvalidHashVersion = errors.New("invalid algorithm version") // stored hash from another argon2 version
)

// SignKey signs and checks the tokens; set it at startup with SetSignKey.
var SignKey []byte

// SetSignKey validates and sets SignKey; call it at startup so a short key
// stops the app.
func SetSignKey(key string) error {
	SignKey = []byte(key)
	_, err := signKey()
	return err
}

func signKey() ([]byte, error) {
	if len(SignKey) < MinKeySize {
		return nil, fmt.Errorf("security: the jwt secret must have at least %d bytes (got %d)", MinKeySize, len(SignKey))
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
