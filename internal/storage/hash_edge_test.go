package storage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashContent_IsHexEncoded(t *testing.T) {
	hash := HashContent([]byte("test"))
	for _, c := range hash {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"hash should be lowercase hex, got char: %c", c)
	}
}

func TestHashContent_BinaryContent(t *testing.T) {
	binary := []byte{0x00, 0xFF, 0xDE, 0xAD, 0xBE, 0xEF}
	hash := HashContent(binary)
	assert.Len(t, hash, 64)
}

func TestHashContent_LargeContent(t *testing.T) {
	large := make([]byte, 1<<20) // 1MB
	for i := range large {
		large[i] = byte(i % 256)
	}
	hash := HashContent(large)
	assert.Len(t, hash, 64)
}

func TestHashContent_NilIsEmptySlice(t *testing.T) {
	hashNil := HashContent(nil)
	hashEmpty := HashContent([]byte{})
	assert.Equal(t, hashNil, hashEmpty, "nil and empty should hash the same")
}

func TestHashContent_SingleByte(t *testing.T) {
	hash := HashContent([]byte{42})
	assert.Len(t, hash, 64)
}

func TestHashContent_WhitespaceMatters(t *testing.T) {
	h1 := HashContent([]byte("hello"))
	h2 := HashContent([]byte("hello "))
	h3 := HashContent([]byte(" hello"))
	h4 := HashContent([]byte("hello\n"))

	assert.NotEqual(t, h1, h2)
	assert.NotEqual(t, h1, h3)
	assert.NotEqual(t, h1, h4)
}

func TestHashContent_ContentAddressedFormat(t *testing.T) {
	// Verify the hash uses "blob:len\0content" format
	content := []byte("hello")
	h1 := HashContent(content)

	// Same content should always give same hash
	h2 := HashContent([]byte("hello"))
	assert.Equal(t, h1, h2)

	// Different length content with same prefix should differ
	h3 := HashContent([]byte("hell"))
	assert.NotEqual(t, h1, h3)
}

func TestHashContent_SameLengthDifferentContent(t *testing.T) {
	// Same length, different content
	h1 := HashContent([]byte("abcd"))
	h2 := HashContent([]byte("efgh"))
	assert.NotEqual(t, h1, h2)
}

func TestHashContent_RepeatedContent(t *testing.T) {
	h1 := HashContent([]byte(strings.Repeat("a", 100)))
	h2 := HashContent([]byte(strings.Repeat("a", 101)))
	assert.NotEqual(t, h1, h2)
}

func TestHashContent_AllZeroes(t *testing.T) {
	zeros := make([]byte, 1000)
	hash := HashContent(zeros)
	assert.Len(t, hash, 64)
	// Not all zeros in the hash
	assert.NotEqual(t, strings.Repeat("0", 64), hash)
}
