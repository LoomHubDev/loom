package merge

// MergeResult represents the result of merging two changes.
type MergeResult struct {
	OK        bool       // true if merge succeeded without conflicts
	Content   []byte     // merged content
	Conflicts []Conflict // conflicts if any
}

// Conflict represents an unresolved merge conflict.
type Conflict struct {
	BaseStart  int      // line number in base
	BaseLines  []string // original lines in base
	OurLines   []string // our version of the lines
	TheirLines []string // their version of the lines
}
