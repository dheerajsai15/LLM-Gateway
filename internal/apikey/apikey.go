package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

const (
	keyPrefix     = "gw_"
	randomBytes   = 32
	displayLength = 12
)

func Generate() string {
	b := make([]byte, randomBytes)
	// Since Go 1.24, rand.Read never returns an error. It crashes the
	// program instead, because a key without real randomness is unsafe.
	rand.Read(b)
	return keyPrefix + base64.RawURLEncoding.EncodeToString(b)
}

func Hash(key string) [sha256.Size]byte {
	return sha256.Sum256([]byte(key))
}

func DisplayPrefix(key string) string {
	return key[:min(len(key), displayLength)]
}
