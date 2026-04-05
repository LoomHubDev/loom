package diff

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextDiff_NoChanges(t *testing.T) {
	content := []byte("line1\nline2\nline3\n")
	hunks := TextDiff(content, content, 3)
	assert.Empty(t, hunks, "identical content should produce no hunks")
}

func TestTextDiff_AddedLines(t *testing.T) {
	old := []byte("line1\nline2\n")
	new := []byte("line1\nline2\nline3\nline4\n")

	hunks := TextDiff(old, new, 3)
	require.Len(t, hunks, 1)

	hunk := hunks[0]
	addLines := linesOfType(hunk, "add")
	assert.Len(t, addLines, 2)
	assert.Equal(t, "line3\n", addLines[0].Content)
	assert.Equal(t, "line4\n", addLines[1].Content)
}

func TestTextDiff_DeletedLines(t *testing.T) {
	old := []byte("line1\nline2\nline3\n")
	new := []byte("line1\n")

	hunks := TextDiff(old, new, 3)
	require.Len(t, hunks, 1)

	hunk := hunks[0]
	delLines := linesOfType(hunk, "delete")
	assert.Len(t, delLines, 2)
	assert.Equal(t, "line2\n", delLines[0].Content)
	assert.Equal(t, "line3\n", delLines[1].Content)
}

func TestTextDiff_ModifiedLines(t *testing.T) {
	old := []byte("line1\noriginal\nline3\n")
	new := []byte("line1\nmodified\nline3\n")

	hunks := TextDiff(old, new, 3)
	require.Len(t, hunks, 1)

	hunk := hunks[0]
	assert.NotEmpty(t, linesOfType(hunk, "delete"), "should have a deleted line")
	assert.NotEmpty(t, linesOfType(hunk, "add"), "should have an added line")

	// The deleted line should contain "original" and the added one "modified".
	del := linesOfType(hunk, "delete")
	add := linesOfType(hunk, "add")
	assert.True(t, strings.Contains(del[0].Content, "original"))
	assert.True(t, strings.Contains(add[0].Content, "modified"))
}

func TestTextDiff_MultipleHunks(t *testing.T) {
	// Build a file where changes are far apart (> 2*contextLines apart).
	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		sb.WriteString("line\n")
	}
	old := []byte(sb.String())

	// Change line 1 and line 20.
	lines := strings.Split(strings.TrimRight(string(old), "\n"), "\n")
	lines[0] = "CHANGED_TOP"
	lines[19] = "CHANGED_BOTTOM"
	new := []byte(strings.Join(lines, "\n") + "\n")

	hunks := TextDiff(old, new, 3)
	assert.Len(t, hunks, 2, "far-apart changes should produce 2 hunks")
}

func TestTextDiff_EmptyOld(t *testing.T) {
	old := []byte{}
	new := []byte("line1\nline2\n")

	hunks := TextDiff(old, new, 3)
	require.Len(t, hunks, 1)

	addLines := linesOfType(hunks[0], "add")
	assert.Len(t, addLines, 2, "all lines should be additions")
}

func TestTextDiff_EmptyNew(t *testing.T) {
	old := []byte("line1\nline2\n")
	new := []byte{}

	hunks := TextDiff(old, new, 3)
	require.Len(t, hunks, 1)

	delLines := linesOfType(hunks[0], "delete")
	assert.Len(t, delLines, 2, "all lines should be deletions")
}

func TestTextDiff_BothEmpty(t *testing.T) {
	hunks := TextDiff([]byte{}, []byte{}, 3)
	assert.Empty(t, hunks, "both empty should produce no hunks")
}

func TestTextDiff_ContextLines(t *testing.T) {
	// Build file: 5 unchanged, 1 changed, 5 unchanged.
	old := []byte("a\nb\nc\nd\ne\nCHANGE\nf\ng\nh\ni\nj\n")
	new := []byte("a\nb\nc\nd\ne\nNEW\nf\ng\nh\ni\nj\n")

	hunks := TextDiff(old, new, 2)
	require.Len(t, hunks, 1)

	ctx := linesOfType(hunks[0], "context")
	// Should have context lines before and after the change (up to 2 each side).
	assert.GreaterOrEqual(t, len(ctx), 2, "should have context lines surrounding the change")
}

func TestTextDiff_SingleLineChange(t *testing.T) {
	old := []byte("only line\n")
	new := []byte("changed line\n")

	hunks := TextDiff(old, new, 3)
	require.Len(t, hunks, 1)

	hunk := hunks[0]
	assert.Equal(t, 1, hunk.OldStart)
	assert.Equal(t, 1, hunk.NewStart)
	assert.NotEmpty(t, linesOfType(hunk, "delete"))
	assert.NotEmpty(t, linesOfType(hunk, "add"))
}

// linesOfType is a test helper that filters DiffLines by type.
func linesOfType(hunk DiffHunk, lineType string) []DiffLine {
	var result []DiffLine
	for _, l := range hunk.Lines {
		if l.Type == lineType {
			result = append(result, l)
		}
	}
	return result
}
