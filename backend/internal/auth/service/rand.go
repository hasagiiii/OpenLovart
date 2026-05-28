package service

import (
	"crypto/rand"
	"fmt"
)

// readRand is a thin wrapper around crypto/rand.Read so service-level
// callers do not import "crypto/rand" directly. Centralizing here also
// makes it trivial to swap in a deterministic source for tests.
func readRand(buf []byte) (int, error) {
	n, err := rand.Read(buf)
	if err != nil {
		return n, fmt.Errorf("service: read rand: %w", err)
	}
	return n, nil
}
