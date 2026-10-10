package security

import (
	"github.com/golang-jwt/jwt/v5"
	"prea/common"
)

const (
	DefaultAlg string = "HS256"
	JwtEnv     string = "JWTKEY"
)

var SignKey []byte

func init() {
	SignKey = []byte(common.GetEnv(JwtEnv))
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

	encoded, err := token.SignedString(SignKey)
	if err != nil {
		return "", err
	}

	return encoded, nil
}

func (Jwt[M]) DecodeToken(token string) (DataClaims[M], error) {
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		return SignKey, nil
	}

	parsed, err := jwt.ParseWithClaims(token, &DataClaims[M]{}, keyFunc)
	if err != nil {
		return DataClaims[M]{}, err
	}

	return *parsed.Claims.(*DataClaims[M]), nil
}
