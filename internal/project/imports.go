package project

import "strings"

// CanonicalImport applies the importing package's authoritative Go import map.
// In particular, standard-library sources can import golang.org/x/... while
// their dependency actually is vendor/golang.org/x/..., distinct from an
// application module providing the same textual import path.
func CanonicalImport(pkg *Package, raw string) string {
	if pkg != nil {
		if canonical := pkg.ImportMap[raw]; canonical != "" {
			// go list annotates test variants with their owning test binary.
			// That suffix is a graph identity, never a compiler import path.
			return strings.Split(canonical, " [")[0]
		}
	}
	return raw
}

// SourceImports adapts canonical package names to one source package's import
// spellings. It does not mutate names or infer package names from path suffixes.
func SourceImports(pkg *Package, names map[string]string) map[string]string {
	result := make(map[string]string, len(names))
	for key, value := range names {
		result[key] = value
	}
	if pkg == nil {
		return result
	}
	for raw, canonical := range pkg.ImportMap {
		name, ok := names[canonical]
		if !ok {
			name, ok = names[strings.Split(canonical, " [")[0]]
		}
		if ok {
			result[raw] = name
		} else {
			delete(result, raw)
		}
	}
	return result
}
