package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"golang.org/x/crypto/argon2"
	"strings"
)

type Argon struct {
	saltlen uint
	mem     uint32
	time    uint32
	threads uint8
	keylen  uint32
}

const (
	SaltLen = 32
	Mem     = 1024 * 64
	Time    = 1
	Threads = 4
	KeyLen  = 32
)

func HashPassword(passwd string) (string, error) {
	return argon2Encoded(passwd, Argon{SaltLen, Mem, Time, Threads, KeyLen})
}

func ComparePassword(passwd string, hashed string) (bool, error) {
	return argon2Compare(passwd, hashed)
}

func genSalt(num uint) ([]byte, error) {
	b := make([]byte, num)
	_, err := rand.Read(b)

	return b, err
}

func argon2Encoded(passwd string, c Argon) (enc string, err error) {
	salt, err := genSalt(c.saltlen)
	if err != nil {
		return "", err
	}

	hashed := argon2.IDKey([]byte(passwd), salt, c.time, c.mem, c.threads, c.keylen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hashed)

	enc = fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, c.mem, c.time, c.threads, b64Salt, b64Hash,
	)

	return
}

func argon2Compare(passwd string, hashed string) (bool, error) {
	vals := strings.Split(hashed, "$")

	if len(vals) != 6 {
		return false, ErrHash{}
	}

	var version int

	_, err := fmt.Sscanf(vals[2], "v=%d", &version)
	if err != nil {
		return false, err
	}

	if version != argon2.Version {
		return false, ErrVer{}
	}

	var mem uint32
	var time uint32
	var threads uint8

	_, err = fmt.Sscanf(vals[3], "m=%d,t=%d,p=%d", &mem, &time, &threads)
	if err != nil {
		return false, err
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(vals[4])
	if err != nil {
		return false, err
	}

	hash, err := base64.RawStdEncoding.Strict().DecodeString(vals[5])
	if err != nil {
		return false, err
	}

	enc := argon2.IDKey([]byte(passwd), salt, time, mem, threads, uint32(len(hash)))

	if subtle.ConstantTimeCompare(enc, hash) == 1 {
		return true, nil
	}

	return false, nil
}

type ErrHash struct{}

func (ErrHash) Error() string {
	return "Invalid hash"
}

type ErrVer struct{}

func (ErrVer) Error() string {
	return "Invalid algorithm version"
}
