package merge

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// ThreeWayMerge performs a three-way merge of base, ours, and theirs.
// It computes line-level diffs from base→ours and base→theirs, then
// combines non-overlapping changes. Overlapping changes that are identical
// are accepted; differing overlapping changes produce conflicts.
func ThreeWayMerge(base, ours, theirs []byte) *MergeResult {
	baseLines := splitLines(string(base))
	ourLines := splitLines(string(ours))
	theirLines := splitLines(string(theirs))

	editsOurs := computeLineEdits(baseLines, ourLines)
	editsTheirs := computeLineEdits(baseLines, theirLines)

	return mergeEdits(baseLines, editsOurs, editsTheirs)
}

// edit represents a line-level change from base to a derived version.
// It replaces base lines [start, end) with newLines.
type edit struct {
	start    int      // start line index in base (inclusive)
	end      int      // end line index in base (exclusive)
	newLines []string // replacement lines
}

// computeLineEdits computes line-level edits from base to derived using
// go-diff's line-mode diffing.
func computeLineEdits(baseLines, derivedLines []string) []edit {
	dmp := diffmatchpatch.New()

	baseText := strings.Join(baseLines, "")
	derivedText := strings.Join(derivedLines, "")

	oldText, newText, lineArray := dmp.DiffLinesToChars(baseText, derivedText)
	diffs := dmp.DiffMain(oldText, newText, false)
	dmp.DiffCleanupSemantic(diffs)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	var edits []edit
	baseIdx := 0

	// We need to group consecutive delete+insert pairs into single edits.
	// Walk through diffs and build edits from non-equal segments.
	i := 0
	for i < len(diffs) {
		d := diffs[i]
		if d.Type == diffmatchpatch.DiffEqual {
			n := countLines(d.Text)
			baseIdx += n
			i++
			continue
		}

		// Collect consecutive non-equal diffs into one edit.
		start := baseIdx
		var newLns []string
		for i < len(diffs) && diffs[i].Type != diffmatchpatch.DiffEqual {
			d = diffs[i]
			switch d.Type {
			case diffmatchpatch.DiffDelete:
				n := countLines(d.Text)
				baseIdx += n
			case diffmatchpatch.DiffInsert:
				newLns = append(newLns, splitLinesRaw(d.Text)...)
			}
			i++
		}
		edits = append(edits, edit{
			start:    start,
			end:      baseIdx,
			newLines: newLns,
		})
	}

	return edits
}

// mergeEdits walks through the base, applying non-overlapping edits from both
// sides and detecting conflicts where edits overlap.
func mergeEdits(baseLines []string, editsA, editsB []edit) *MergeResult {
	var merged []string
	var conflicts []Conflict

	ia, ib := 0, 0
	basePos := 0

	for ia < len(editsA) || ib < len(editsB) {
		var nextA, nextB *edit
		if ia < len(editsA) {
			nextA = &editsA[ia]
		}
		if ib < len(editsB) {
			nextB = &editsB[ib]
		}

		// If only one side has edits left, or one edit comes strictly before the other.
		if nextA != nil && nextB != nil {
			// Pure insertions at the same point (start == end for both, same position)
			// must be treated as overlapping, not sequential.
			samePoint := nextA.start == nextA.end && nextB.start == nextB.end &&
				nextA.start == nextB.start

			// Check for overlap: two edits overlap if their base ranges intersect
			// or if both are pure insertions at the same point.
			if !samePoint && nextA.end <= nextB.start {
				// A comes first, no overlap.
				merged = append(merged, baseLines[basePos:nextA.start]...)
				merged = append(merged, nextA.newLines...)
				basePos = nextA.end
				ia++
				continue
			}
			if !samePoint && nextB.end <= nextA.start {
				// B comes first, no overlap.
				merged = append(merged, baseLines[basePos:nextB.start]...)
				merged = append(merged, nextB.newLines...)
				basePos = nextB.end
				ib++
				continue
			}

			// Overlapping edits. Check if they are identical.
			overlapStart := min(nextA.start, nextB.start)
			overlapEnd := max(nextA.end, nextB.end)

			if sameLines(nextA.newLines, nextB.newLines) &&
				nextA.start == nextB.start && nextA.end == nextB.end {
				// Identical changes — take either.
				merged = append(merged, baseLines[basePos:nextA.start]...)
				merged = append(merged, nextA.newLines...)
				basePos = nextA.end
				ia++
				ib++
				continue
			}

			// Conflict.
			merged = append(merged, baseLines[basePos:overlapStart]...)
			conflicts = append(conflicts, Conflict{
				BaseStart:  overlapStart,
				BaseLines:  copySlice(baseLines[overlapStart:overlapEnd]),
				OurLines:   copySlice(nextA.newLines),
				TheirLines: copySlice(nextB.newLines),
			})
			basePos = overlapEnd
			ia++
			ib++
			continue
		}

		// Only one side has remaining edits.
		if nextA != nil {
			merged = append(merged, baseLines[basePos:nextA.start]...)
			merged = append(merged, nextA.newLines...)
			basePos = nextA.end
			ia++
			continue
		}
		if nextB != nil {
			merged = append(merged, baseLines[basePos:nextB.start]...)
			merged = append(merged, nextB.newLines...)
			basePos = nextB.end
			ib++
			continue
		}
	}

	// Append remaining base lines.
	if basePos < len(baseLines) {
		merged = append(merged, baseLines[basePos:]...)
	}

	content := []byte(strings.Join(merged, ""))

	return &MergeResult{
		OK:        len(conflicts) == 0,
		Content:   content,
		Conflicts: conflicts,
	}
}

// splitLines splits text into lines, preserving trailing newlines.
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

// splitLinesRaw is the same as splitLines but for diff output text.
func splitLinesRaw(text string) []string {
	return splitLines(text)
}

// countLines counts the number of lines in text (each line ends with \n,
// except possibly the last one).
func countLines(text string) int {
	if text == "" {
		return 0
	}
	n := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}

// sameLines returns true if two slices of line strings are identical.
func sameLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copySlice returns a copy of the string slice.
func copySlice(s []string) []string {
	if s == nil {
		return nil
	}
	c := make([]string, len(s))
	copy(c, s)
	return c
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

