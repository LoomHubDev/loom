package storage

import (
	"crypto/sha256"
	"encoding/hex"
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
