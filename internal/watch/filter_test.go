package watch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilter_IgnoresConfiguredPatterns(t *testing.T) {
	f := NewFilter([]string{".git", "node_modules", "*.tmp", "*.swp"}, "")

	assert.True(t, f.ShouldIgnore(".git"), ".git should be ignored")
	assert.True(t, f.ShouldIgnore("node_modules"), "node_modules should be ignored")
	assert.True(t, f.ShouldIgnore("file.tmp"), "*.tmp should be ignored")
	assert.True(t, f.ShouldIgnore("file.swp"), "*.swp should be ignored")

	assert.False(t, f.ShouldIgnore("main.go"), "main.go should not be ignored")
}

func TestFilter_IgnoresNestedPaths(t *testing.T) {
	f := NewFilter([]string{".git", "node_modules"}, "")

	assert.True(t, f.ShouldIgnore(".git/config"), ".git/config should be ignored")
	assert.True(t, f.ShouldIgnore(".git/objects/ab/cd1234"), ".git/objects/ab/cd1234 should be ignored")
	assert.True(t, f.ShouldIgnore("node_modules/express/index.js"), "node_modules/express/index.js should be ignored")
}

func TestFilter_LoomignoreFile(t *testing.T) {
	dir := t.TempDir()
	loomignorePath := filepath.Join(dir, ".loomignore")

	content := "build/\n*.log\n# comment\n\n"
	err := os.WriteFile(loomignorePath, []byte(content), 0644)
	require.NoError(t, err)

	f := NewFilter(nil, dir)

	assert.True(t, f.ShouldIgnore("build/output.js"), "build/output.js should be ignored")
	assert.True(t, f.ShouldIgnore("server.log"), "server.log should be ignored")
	assert.False(t, f.ShouldIgnore("main.go"), "main.go should not be ignored")
}

func TestFilter_NoLoomignoreFile(t *testing.T) {
	dir := t.TempDir()
	// No .loomignore written — should not panic or error.

	f := NewFilter([]string{"*.tmp"}, dir)

	assert.True(t, f.ShouldIgnore("file.tmp"))
	assert.False(t, f.ShouldIgnore("file.go"))
}

func TestFilter_EmptyRules(t *testing.T) {
	f := NewFilter(nil, "")
	// Only .loom is always added; nil config rules means only .loom.
	// Check that arbitrary files are not ignored.
	assert.False(t, f.ShouldIgnore("main.go"))
	assert.False(t, f.ShouldIgnore("README.md"))
	assert.False(t, f.ShouldIgnore("node_modules/express/index.js"))
}

func TestFilter_AlwaysIgnoresLoomDir(t *testing.T) {
	f := NewFilter(nil, "")

	assert.True(t, f.ShouldIgnore(".loom"), ".loom dir should always be ignored")
	assert.True(t, f.ShouldIgnore(".loom/loom.db"), ".loom/loom.db should always be ignored")
}

func TestFilter_RouteToSpace(t *testing.T) {
	spaces := map[string]string{
		"docs":   "docs",
		"design": "design",
		"code":   ".",
	}

	f := NewFilter(nil, "")

	assert.Equal(t, "docs", f.RouteToSpace("docs/readme.md", spaces))
	assert.Equal(t, "design", f.RouteToSpace("design/mock.json", spaces))
	assert.Equal(t, "code", f.RouteToSpace("src/main.go", spaces))
	assert.Equal(t, "code", f.RouteToSpace("main.go", spaces))
}

func TestFilter_RouteToSpace_LongestMatchWins(t *testing.T) {
	spaces := map[string]string{
		"docs": "docs",
		"code": ".",
	}

	f := NewFilter(nil, "")

	// "docs/readme.md" should match "docs" (length 4) not "." (length 0).
	result := f.RouteToSpace("docs/readme.md", spaces)
	assert.Equal(t, "docs", result, "longest prefix match should win")
}

func TestFilter_RouteToSpace_NoMatch(t *testing.T) {
	f := NewFilter(nil, "")

	assert.Equal(t, "", f.RouteToSpace("main.go", nil))
	assert.Equal(t, "", f.RouteToSpace("main.go", map[string]string{}))
}
