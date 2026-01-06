package core

import (
	"crypto/rand"
	"encoding/hex"
)

func newID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "tab-unknown"
	}
	return hex.EncodeToString(buf[:])
}
