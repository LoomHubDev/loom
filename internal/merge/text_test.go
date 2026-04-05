package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThreeWayMerge_NoChanges(t *testing.T) {
	base := []byte("line1\nline2\nline3\n")
	result := ThreeWayMerge(base, base, base)

	require.True(t, result.OK)
	assert.Equal(t, base, result.Content)
	assert.Empty(t, result.Conflicts)
}

func TestThreeWayMerge_OnlyOursChanged(t *testing.T) {
	base := []byte("line1\nline2\nline3\n")
	ours := []byte("line1\nmodified\nline3\n")

	result := ThreeWayMerge(base, ours, base)

	require.True(t, result.OK)
	assert.Equal(t, ours, result.Content)
	assert.Empty(t, result.Conflicts)
}

func TestThreeWayMerge_OnlyTheirsChanged(t *testing.T) {
	base := []byte("line1\nline2\nline3\n")
	theirs := []byte("line1\nmodified\nline3\n")

	result := ThreeWayMerge(base, base, theirs)

	require.True(t, result.OK)
	assert.Equal(t, theirs, result.Content)
	assert.Empty(t, result.Conflicts)
}

func TestThreeWayMerge_BothChangedDifferentRegions(t *testing.T) {
	base := []byte("line1\nline2\nline3\nline4\nline5\n")
	ours := []byte("line1\nmodified2\nline3\nline4\nline5\n")   // changed line 2
	theirs := []byte("line1\nline2\nline3\nline4\nchanged5\n") // changed line 5

	result := ThreeWayMerge(base, ours, theirs)

	require.True(t, result.OK)
	expected := []byte("line1\nmodified2\nline3\nline4\nchanged5\n")
	assert.Equal(t, expected, result.Content)
	assert.Empty(t, result.Conflicts)
}

func TestThreeWayMerge_BothChangedSameIdentically(t *testing.T) {
	base := []byte("line1\nline2\nline3\n")
	changed := []byte("line1\nSAME\nline3\n") // both made identical change

	result := ThreeWayMerge(base, changed, changed)

	require.True(t, result.OK)
	assert.Equal(t, changed, result.Content)
	assert.Empty(t, result.Conflicts)
}

func TestThreeWayMerge_Conflict(t *testing.T) {
	base := []byte("line1\nline2\nline3\n")
	ours := []byte("line1\nours2\nline3\n")
	theirs := []byte("line1\ntheirs2\nline3\n")

	result := ThreeWayMerge(base, ours, theirs)

	require.False(t, result.OK)
	require.Len(t, result.Conflicts, 1)

	c := result.Conflicts[0]
	assert.Equal(t, []string{"line2\n"}, c.BaseLines)
	assert.Equal(t, []string{"ours2\n"}, c.OurLines)
	assert.Equal(t, []string{"theirs2\n"}, c.TheirLines)
}

func TestThreeWayMerge_AddedLines(t *testing.T) {
	base := []byte("line1\nline2\n")
	ours := []byte("line1\nline2\nline3\nline4\n") // added lines at end

	result := ThreeWayMerge(base, ours, base)

	require.True(t, result.OK)
	assert.Equal(t, ours, result.Content)
	assert.Empty(t, result.Conflicts)
}

func TestThreeWayMerge_DeletedLines(t *testing.T) {
	base := []byte("line1\nline2\nline3\n")
	ours := []byte("line1\nline3\n") // deleted line 2

	result := ThreeWayMerge(base, ours, base)

	require.True(t, result.OK)
	assert.Equal(t, ours, result.Content)
	assert.Empty(t, result.Conflicts)
}

func TestThreeWayMerge_EmptyBase(t *testing.T) {
	base := []byte("")

	t.Run("both add same content", func(t *testing.T) {
		added := []byte("new line\n")
		result := ThreeWayMerge(base, added, added)

		require.True(t, result.OK)
		assert.Equal(t, added, result.Content)
	})

	t.Run("both add different content", func(t *testing.T) {
		ours := []byte("our line\n")
		theirs := []byte("their line\n")
		result := ThreeWayMerge(base, ours, theirs)

		require.False(t, result.OK)
		require.NotEmpty(t, result.Conflicts)
	})
}

func TestThreeWayMerge_MultipleConflicts(t *testing.T) {
	base := []byte("aaa\nbbb\nccc\nddd\neee\nfff\nggg\n")
	ours := []byte("aaa\nBBB-ours\nccc\nddd\neee\nFFF-ours\nggg\n")
	theirs := []byte("aaa\nBBB-theirs\nccc\nddd\neee\nFFF-theirs\nggg\n")

	result := ThreeWayMerge(base, ours, theirs)

	require.False(t, result.OK)
	require.Len(t, result.Conflicts, 2, "expected 2 separate conflicts")

	assert.Equal(t, []string{"bbb\n"}, result.Conflicts[0].BaseLines)
	assert.Equal(t, []string{"BBB-ours\n"}, result.Conflicts[0].OurLines)
	assert.Equal(t, []string{"BBB-theirs\n"}, result.Conflicts[0].TheirLines)

	assert.Equal(t, []string{"fff\n"}, result.Conflicts[1].BaseLines)
	assert.Equal(t, []string{"FFF-ours\n"}, result.Conflicts[1].OurLines)
	assert.Equal(t, []string{"FFF-theirs\n"}, result.Conflicts[1].TheirLines)
}
