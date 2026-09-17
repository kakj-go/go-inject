// Package rewrite applies declarative Go injection templates to a package.
package rewrite

// Source is one selected, buildable Go source file in the target package.
type Source struct {
	Path string
	Data []byte
	// ImportNames maps import paths to authoritative package names from go list.
	ImportNames map[string]string
}

// Rule is a selected template. Target names a package ("main" for the
// application entry); File optionally narrows a file target to one named
// source file within that package, and is empty for package-level targets
// whose declarations match anywhere in the package.
// Provider and ID distinguish independently distributed injection templates.
type Rule struct {
	Path, Target, File, Provider, ID string
	Source                           []byte
	ImportNames                      map[string]string
	// Bindings is populated by Go type checking. Syntax pairs ensure workers
	// apply the proof to exactly the declarations which were checked.
	Bindings map[string]Binding
}

type Binding struct{ Template, Target string }

// Match records an applied declaration or function injection.
type Match struct {
	Rule, Target, Function string
	Order                  int
}

// Link describes a native go:linkname declaration that needs link closure.
type Link struct {
	Symbol, Signature string
	Checked           bool
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
