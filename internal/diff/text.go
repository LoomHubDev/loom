package diff

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// TextDiff computes a line-based diff between oldContent and newContent,
// returning a slice of DiffHunks. contextLines specifies how many surrounding
// unchanged lines to include around each change; if <= 0, defaults to 3.
func TextDiff(oldContent, newContent []byte, contextLines int) []DiffHunk {
	if contextLines <= 0 {
		contextLines = 3
	}

	oldLines := splitLines(string(oldContent))
	newLines := splitLines(string(newContent))

	dmp := diffmatchpatch.New()

	// Use line-mode diff for efficiency and cleaner results.
	oldText, newText, lineArray := dmp.DiffLinesToChars(
		strings.Join(oldLines, ""),
		strings.Join(newLines, ""),
	)
	diffs := dmp.DiffMain(oldText, newText, false)
	dmp.DiffCleanupSemantic(diffs)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	// Expand diffs into a flat list of tagged lines.
	type taggedLine struct {
		tag     string // "add", "delete", "context"
		content string
		oldIdx  int // 1-based line number in old file (-1 for adds)
		newIdx  int // 1-based line number in new file (-1 for deletes)
	}

	var flat []taggedLine
	oldPos := 1
	newPos := 1

	for _, d := range diffs {
		lines := splitLines(d.Text)
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			for _, l := range lines {
				flat = append(flat, taggedLine{"context", l, oldPos, newPos})
				oldPos++
				newPos++
			}
		case diffmatchpatch.DiffDelete:
			for _, l := range lines {
				flat = append(flat, taggedLine{"delete", l, oldPos, -1})
				oldPos++
			}
		case diffmatchpatch.DiffInsert:
			for _, l := range lines {
				flat = append(flat, taggedLine{"add", l, -1, newPos})
				newPos++
			}
		}
	}

	if len(flat) == 0 {
		return nil
	}

	// Identify indices of changed lines.
	changed := make([]bool, len(flat))
	for i, tl := range flat {
		if tl.tag != "context" {
			changed[i] = true
		}
	}

	// Build hunk ranges by expanding each change with contextLines.
	type hunkRange struct{ start, end int }
	var ranges []hunkRange

	i := 0
	for i < len(flat) {
		if !changed[i] {
			i++
			continue
		}
		// Start of a new change region.
		start := i - contextLines
		if start < 0 {
			start = 0
		}
		// Extend end past the change, then look for more nearby changes.
		end := i
		for end < len(flat) {
			if changed[end] {
				end++
				continue
			}
			// Count consecutive context lines after last change.
			contextAfter := 0
			j := end
			for j < len(flat) && !changed[j] {
				contextAfter++
				j++
			}
			if contextAfter <= contextLines*2 && j < len(flat) {
				// Gap is small enough — merge into this hunk.
				end = j
				continue
			}
			break
		}
		// Include trailing context.
		end = end + contextLines
		if end > len(flat) {
			end = len(flat)
		}
		ranges = append(ranges, hunkRange{start, end})
		i = end
	}

	// Convert ranges to DiffHunks.
	var hunks []DiffHunk
	for _, r := range ranges {
		hunk := DiffHunk{}
		firstOld := -1
		firstNew := -1

		for _, tl := range flat[r.start:r.end] {
			if tl.tag != "add" && firstOld == -1 {
				firstOld = tl.oldIdx
			}
			if tl.tag != "delete" && firstNew == -1 {
				firstNew = tl.newIdx
			}
		}
		if firstOld == -1 {
			// Pure additions — hunk starts after last old line before this range.
			firstOld = 0
			for _, tl := range flat[:r.start] {
				if tl.oldIdx > 0 {
					firstOld = tl.oldIdx
				}
			}
			firstOld++ // insert after
		}
		if firstNew == -1 {
			firstNew = 0
			for _, tl := range flat[:r.start] {
				if tl.newIdx > 0 {
					firstNew = tl.newIdx
				}
			}
			firstNew++
		}

		hunk.OldStart = firstOld
		hunk.NewStart = firstNew

		for _, tl := range flat[r.start:r.end] {
			hunk.Lines = append(hunk.Lines, DiffLine{Type: tl.tag, Content: tl.content})
			if tl.tag != "add" {
				hunk.OldLines++
			}
			if tl.tag != "delete" {
				hunk.NewLines++
			}
		}

		hunks = append(hunks, hunk)
	}

	return hunks
}

// splitLines splits text into lines, preserving the trailing newline on each
// line. An empty string returns an empty slice.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	var lines []string
	for len(text) > 0 {
		idx := strings.Index(text, "\n")
		if idx == -1 {
			lines = append(lines, text)
			break
		}
		lines = append(lines, text[:idx+1])
		text = text[idx+1:]
	}
	return lines
}
