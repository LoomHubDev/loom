package diff

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/constructspace/loom/internal/core"
)

// FormatTerminal formats a DiffResult for terminal output.
// If color is true, ANSI color codes are used for adds/deletes.
func FormatTerminal(result *DiffResult, color bool) string {
	if result == nil {
		return ""
	}

	var b strings.Builder

	// Summary line.
	fmt.Fprintf(&b, "diff %s..%s\n", result.FromRef, result.ToRef)
	fmt.Fprintf(&b, "%d space(s) changed, %d operation(s)\n\n",
		result.Summary.SpacesChanged, result.Summary.TotalOperations)

	for _, space := range result.Spaces {
		// Space header.
		fmt.Fprintf(&b, "─── %s ───  +%d ~%d -%d\n",
			space.SpaceID,
			space.Summary.EntitiesCreated,
			space.Summary.EntitiesModified,
			space.Summary.EntitiesDeleted,
		)

		for _, entity := range space.Entities {
			icon := changeIcon(entity.Change)
			if color {
				icon = colorIcon(entity.Change, icon)
			}
			fmt.Fprintf(&b, "  %s %s\n", icon, entity.Path)

			// Print hunks for entities with content diffs.
			for _, hunk := range entity.Hunks {
				fmt.Fprintf(&b, "    @@ -%d,%d +%d,%d @@\n",
					hunk.OldStart, hunk.OldLines,
					hunk.NewStart, hunk.NewLines,
				)
				for _, line := range hunk.Lines {
					formatted := formatDiffLine(line, color)
					fmt.Fprintf(&b, "    %s\n", formatted)
				}
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

// FormatJSON formats a DiffResult as indented JSON.
func FormatJSON(result *DiffResult) string {
	if result == nil {
		return "{}"
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(data)
}

// changeIcon returns the icon prefix for a change type.
func changeIcon(change core.ChangeType) string {
	switch change {
	case core.ChangeCreated:
		return "+"
	case core.ChangeDeleted:
		return "-"
	case core.ChangeMoved:
		return ">"
	default:
		return "M"
	}
}

// colorIcon wraps an icon in ANSI color codes.
func colorIcon(change core.ChangeType, icon string) string {
	switch change {
	case core.ChangeCreated:
		return "\033[32m" + icon + "\033[0m" // green
	case core.ChangeDeleted:
		return "\033[31m" + icon + "\033[0m" // red
	case core.ChangeMoved:
		return "\033[33m" + icon + "\033[0m" // yellow
	default:
		return "\033[36m" + icon + "\033[0m" // cyan
	}
}

// formatDiffLine formats a single diff line with optional color.
func formatDiffLine(line DiffLine, color bool) string {
	content := strings.TrimRight(line.Content, "\n")
	switch line.Type {
	case "add":
		if color {
			return "\033[32m+" + content + "\033[0m"
		}
		return "+" + content
	case "delete":
		if color {
			return "\033[31m-" + content + "\033[0m"
		}
		return "-" + content
	default:
		return " " + content
	}
}
