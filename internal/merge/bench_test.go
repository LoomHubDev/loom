package merge

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkThreeWayMerge_NoConflict(b *testing.B) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	base := []byte(strings.Join(lines, "\n") + "\n")

	oursLines := make([]string, len(lines))
	copy(oursLines, lines)
	oursLines[10] = "line 10 modified by ours"
	ours := []byte(strings.Join(oursLines, "\n") + "\n")

	theirsLines := make([]string, len(lines))
	copy(theirsLines, lines)
	theirsLines[90] = "line 90 modified by theirs"
	theirs := []byte(strings.Join(theirsLines, "\n") + "\n")

	b.ResetTimer()
	for b.Loop() {
		ThreeWayMerge(base, ours, theirs)
	}
}

func BenchmarkThreeWayMerge_WithConflict(b *testing.B) {
	base := []byte("line1\nline2\nline3\n")
	ours := []byte("line1\nours\nline3\n")
	theirs := []byte("line1\ntheirs\nline3\n")

	b.ResetTimer()
	for b.Loop() {
		ThreeWayMerge(base, ours, theirs)
	}
}
