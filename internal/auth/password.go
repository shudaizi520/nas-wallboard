package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	minimumPasswordBytes = 12
	passwordMemoryKiB    = 32 * 1024
	passwordIterations   = 3
	passwordParallelism  = 2
	passwordSaltBytes    = 16
	passwordHashBytes    = 32
)

var ErrPasswordTooShort = errors.New("password must contain at least 12 characters")

type Record struct {
	Algorithm   string `json:"algorithm"`
	Version     int    `json:"version"`
	Memory      uint32 `json:"memory_kib"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	Salt        string `json:"salt"`
	Hash        string `json:"hash"`
}

func HashPassword(password []byte) (Record, error) {
	if !utf8.Valid(password) || utf8.RuneCount(password) < minimumPasswordBytes {
		return Record{}, ErrPasswordTooShort
	}
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return Record{}, err
	}
	hash := argon2.IDKey(password, salt, passwordIterations, passwordMemoryKiB, passwordParallelism, passwordHashBytes)
	return Record{
		Algorithm: "argon2id", Version: argon2.Version, Memory: passwordMemoryKiB,
		Iterations: passwordIterations, Parallelism: passwordParallelism,
		Salt: base64.RawStdEncoding.EncodeToString(salt), Hash: base64.RawStdEncoding.EncodeToString(hash),
	}, nil
}

func VerifyPassword(record Record, password []byte) bool {
	salt := make([]byte, passwordSaltBytes)
	expected := make([]byte, passwordHashBytes)
	valid := record.Algorithm == "argon2id" && record.Version == argon2.Version &&
		record.Memory >= 8*1024 && record.Memory <= 128*1024 &&
		record.Iterations >= 1 && record.Iterations <= 10 &&
		record.Parallelism >= 1 && record.Parallelism <= 8
	if decoded, err := base64.RawStdEncoding.DecodeString(record.Salt); err == nil && len(decoded) == passwordSaltBytes {
		copy(salt, decoded)
	} else {
		valid = false
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(record.Hash); err == nil && len(decoded) == passwordHashBytes {
		copy(expected, decoded)
	} else {
		valid = false
	}
	memory, iterations, parallelism := record.Memory, record.Iterations, record.Parallelism
	if !valid {
		memory, iterations, parallelism = passwordMemoryKiB, passwordIterations, passwordParallelism
	}
	actual := argon2.IDKey(password, salt, iterations, memory, parallelism, passwordHashBytes)
	return valid && subtle.ConstantTimeCompare(actual, expected) == 1
}
