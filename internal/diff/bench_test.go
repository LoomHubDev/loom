package diff

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkTextDiff_SmallChange(b *testing.B) {
	old := []byte("line1\nline2\nline3\nline4\nline5\n")
	new := []byte("line1\nmodified\nline3\nline4\nline5\n")

	b.ResetTimer()
	for b.Loop() {
		TextDiff(old, new, 3)
	}
}

func BenchmarkTextDiff_LargeFile(b *testing.B) {
	var oldLines, newLines []string
	for i := 0; i < 1000; i++ {
		oldLines = append(oldLines, fmt.Sprintf("line %d: original content here", i))
		if i%100 == 50 {
			newLines = append(newLines, fmt.Sprintf("line %d: MODIFIED content here", i))
		} else {
			newLines = append(newLines, fmt.Sprintf("line %d: original content here", i))
		}
	}
	old := []byte(strings.Join(oldLines, "\n"))
	new := []byte(strings.Join(newLines, "\n"))

	b.ResetTimer()
	for b.Loop() {
		TextDiff(old, new, 3)
	}
}
