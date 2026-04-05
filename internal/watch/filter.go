package watch

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Filter determines whether file events should be ignored and routes files to spaces.
type Filter struct {
	rules []string
}

// NewFilter creates a Filter combining the always-ignored ".loom" dir,
// configRules from project config, and any rules from a .loomignore file
// at projectPath.
func NewFilter(configRules []string, projectPath string) *Filter {
	rules := []string{".loom"}
	rules = append(rules, configRules...)

	if projectPath != "" {
		loomignorePath := filepath.Join(projectPath, ".loomignore")
		if extra, err := readLoomignore(loomignorePath); err == nil {
			rules = append(rules, extra...)
		}
	}

	return &Filter{rules: rules}
}

// ShouldIgnore returns true if the given relative path matches any ignore rule.
// It checks each path component and the full path against all rules using
// filepath.Match.
func (f *Filter) ShouldIgnore(relPath string) bool {
	if len(f.rules) == 0 {
		return false
	}

	// Normalize to forward slashes for consistent component splitting.
	normalised := filepath.ToSlash(relPath)
	parts := strings.Split(normalised, "/")

	for _, rule := range f.rules {
		// Check each component against the rule (handles ".git", "node_modules", etc.)
		for _, part := range parts {
			if matched, _ := filepath.Match(rule, part); matched {
				return true
			}
		}
		// Also check the full path against the rule.
		if matched, _ := filepath.Match(rule, normalised); matched {
			return true
		}
		// Check the full path with the rule as a prefix directory pattern.
		ruleSlash := filepath.ToSlash(rule)
		if strings.HasPrefix(normalised, ruleSlash+"/") || normalised == ruleSlash {
			return true
		}
	}

	return false
}

// RouteToSpace returns the spaceID whose path is the best (longest) prefix
// match for relPath. spaces maps spaceID -> spacePath. The root path "." is
// treated as lowest priority. Returns "" when spaces is nil or empty, or when
// no space matches.
func (f *Filter) RouteToSpace(relPath string, spaces map[string]string) string {
	if len(spaces) == 0 {
		return ""
	}

	normalised := filepath.ToSlash(relPath)

	bestID := ""
	bestLen := -1

	for id, spacePath := range spaces {
		sp := filepath.ToSlash(spacePath)

		var matched bool
		var matchLen int

		if sp == "." {
			// Root space matches everything, but with the lowest priority (length 0).
			matched = true
			matchLen = 0
		} else {
			// A space matches if the path starts with spacePath + "/" or equals it.
			if strings.HasPrefix(normalised, sp+"/") || normalised == sp {
				matched = true
				matchLen = len(sp)
			}
		}

		if matched && matchLen > bestLen {
			bestLen = matchLen
			bestID = id
		}
	}

	return bestID
}

// readLoomignore reads a .loomignore file and returns non-empty, non-comment
// lines with trailing slashes stripped.
func readLoomignore(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rules []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimRight(line, "/")
		rules = append(rules, line)
	}

	return rules, scanner.Err()
}
