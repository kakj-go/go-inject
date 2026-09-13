// Package rewrite applies declarative Go injection templates to a package.
package rewrite

// Source is one selected, buildable Go source file in the target package.
type Source struct {
	Path string
	Data []byte
	// ImportNames maps import paths to authoritative package names from go list.
	ImportNames map[string]string
}

// Rule is a selected template. Target names a package/file.go or main.
// Provider and ID distinguish independently distributed injection templates.
type Rule struct {
	Path, Target, Provider, ID string
	Source                     []byte
	ImportNames                map[string]string
}

// Match records an applied declaration or function injection.
type Match struct {
	Rule, Target, Function string
	Order                  int
}

// Link describes a native go:linkname declaration that needs link closure.
type Link struct {
	Symbol, Signature string
}

// Result contains only changed source files and newly added Go files.
// Additions with a main/ prefix must be routed to the application's main package.
type Result struct {
	Replacements map[string][]byte
	Additions    map[string][]byte
	Imports      []string
	Links        []Link
	Matches      []Match
}
