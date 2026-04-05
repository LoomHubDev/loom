package merge

import (
	"encoding/json"
	"sort"
)

// StructuredMerge performs a field-level three-way merge on JSON objects.
// If any input fails to parse as a JSON object, it falls back to ThreeWayMerge.
// Only flat key comparison is performed — nested objects are compared as whole values.
func StructuredMerge(base, ours, theirs []byte) *MergeResult {
	var baseMap, oursMap, theirsMap map[string]any

	if err := json.Unmarshal(base, &baseMap); err != nil {
		return ThreeWayMerge(base, ours, theirs)
	}
	if err := json.Unmarshal(ours, &oursMap); err != nil {
		return ThreeWayMerge(base, ours, theirs)
	}
	if err := json.Unmarshal(theirs, &theirsMap); err != nil {
		return ThreeWayMerge(base, ours, theirs)
	}

	// Collect all keys across all three maps.
	allKeys := make(map[string]struct{})
	for k := range baseMap {
		allKeys[k] = struct{}{}
	}
	for k := range oursMap {
		allKeys[k] = struct{}{}
	}
	for k := range theirsMap {
		allKeys[k] = struct{}{}
	}

	result := make(map[string]any)
	var conflicts []Conflict

	for key := range allKeys {
		baseVal, inBase := baseMap[key]
		oursVal, inOurs := oursMap[key]
		theirsVal, inTheirs := theirsMap[key]

		baseJSON := jsonBytes(baseVal, inBase)
		oursJSON := jsonBytes(oursVal, inOurs)
		theirsJSON := jsonBytes(theirsVal, inTheirs)

		oursChanged := !jsonEqual(baseJSON, oursJSON) || inBase != inOurs
		theirsChanged := !jsonEqual(baseJSON, theirsJSON) || inBase != inTheirs

		switch {
		case !oursChanged && !theirsChanged:
			// No changes — keep base value.
			if inBase {
				result[key] = baseVal
			}

		case oursChanged && !theirsChanged:
			// Only ours changed.
			if inOurs {
				result[key] = oursVal
			}
			// If ours deleted the key (inOurs == false), don't add it.

		case !oursChanged && theirsChanged:
			// Only theirs changed.
			if inTheirs {
				result[key] = theirsVal
			}
			// If theirs deleted the key, don't add it.

		case oursChanged && theirsChanged:
			// Both changed.
			if jsonEqual(oursJSON, theirsJSON) && inOurs == inTheirs {
				// Same change — take either.
				if inOurs {
					result[key] = oursVal
				}
			} else {
				// Conflict.
				conflicts = append(conflicts, Conflict{
					BaseStart:  0,
					BaseLines:  []string{key + ": " + string(baseJSON)},
					OurLines:   []string{key + ": " + string(oursJSON)},
					TheirLines: []string{key + ": " + string(theirsJSON)},
				})
				// Keep base value in result so output is still valid JSON.
				if inBase {
					result[key] = baseVal
				}
			}
		}
	}

	// Sort conflicts by key for deterministic output.
	sort.Slice(conflicts, func(i, j int) bool {
		return conflicts[i].BaseLines[0] < conflicts[j].BaseLines[0]
	})

	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		// Should never happen with map[string]any, but be safe.
		return ThreeWayMerge(base, ours, theirs)
	}
	content = append(content, '\n')

	return &MergeResult{
		OK:        len(conflicts) == 0,
		Content:   content,
		Conflicts: conflicts,
	}
}

// jsonBytes marshals a value to JSON bytes. If not present, returns "null".
func jsonBytes(val any, present bool) []byte {
	if !present {
		return nil
	}
	b, _ := json.Marshal(val)
	return b
}

// jsonEqual compares two JSON byte slices for equality.
func jsonEqual(a, b []byte) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Normalize via re-marshal to handle formatting differences.
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return string(a) == string(b)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return string(a) == string(b)
	}
	na, _ := json.Marshal(va)
	nb, _ := json.Marshal(vb)
	return string(na) == string(nb)
}
