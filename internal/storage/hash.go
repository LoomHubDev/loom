package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// HashContent computes a SHA-256 hash of content with a type prefix.
// Format: sha256("blob:" + len + "\0" + content)
func HashContent(content []byte) string {
	h := sha256.New()
	h.Write([]byte("blob:"))
	h.Write([]byte(strconv.Itoa(len(content))))
	h.Write([]byte{0})
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// ValidateHash returns an error if hash is not a valid SHA-256 hex digest.
func ValidateHash(hash string) error {
	if len(hash) != 64 {
		return fmt.Errorf("invalid hash length %d", len(hash))
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return fmt.Errorf("invalid hash encoding: %w", err)
	}
	return nil
}
