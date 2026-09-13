package project

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kakj-go/go-inject/internal/process"
)

type flagSpec struct{ value, list, testAlias bool }

// This is the build/test flag surface of the two supported Go series. Unknown
// flags fail before interpreting their following argument as a package name.
var goFlags = func() map[string]flagSpec {
	m := make(map[string]flagSpec)
	add := func(names string, spec flagSpec) {
		for _, name := range strings.Fields(names) {
			m[name] = spec
		}
	}
	add("a n x v work json c h help artifacts benchmem failfast fullpath short", flagSpec{})
	add("race msan asan trimpath buildvcs linkshared modcacherw cover", flagSpec{list: true})
	add("C o exec vet toolexec debug-actiongraph debug-runtime-trace debug-trace", flagSpec{value: true})
	add("p tags mod modfile overlay compiler pkgdir installsuffix gcflags asmflags ldflags gccgoflags pgo buildmode covermode coverpkg", flagSpec{value: true, list: true})
	add("run skip bench benchtime count timeout parallel cpu shuffle list fuzz fuzztime fuzzminimizetime coverprofile cpuprofile memprofile blockprofile mutexprofile trace outputdir memprofilerate blockprofilerate mutexprofilefraction", flagSpec{value: true})
	for _, name := range strings.Fields("artifacts bench benchmem benchtime blockprofile blockprofilerate count cpu cpuprofile failfast fullpath fuzz list memprofile memprofilerate mutexprofile mutexprofilefraction outputdir parallel run short skip timeout fuzztime fuzzminimizetime trace v shuffle coverprofile") {
		spec := m[name]
		spec.testAlias = true
		m[name] = spec
	}
	return m
}()

func flagName(arg string) (name, value string, hasValue bool) {
	name, value, hasValue = strings.Cut(strings.TrimLeft(arg, "-"), "=")
	if alias, ok := strings.CutPrefix(name, "test."); ok && goFlags[alias].testAlias {
		name = alias
	}
	return
}

// SplitFlags separates already-tokenized Go flags and entry package patterns.
// Custom test flags belong after -args; unknown flags before it are errors.
func SplitFlags(args []string) (flags, roots []string, err error) {
	flags, roots, err = splitFlags(args, false)
	if err == nil && len(roots) == 0 {
		roots = []string{"."}
	}
	if err == nil {
		err = validateBuildTags(flags)
	}
	return
}

func splitFlags(args []string, environment bool) (flags, roots []string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-args" || arg == "--args" {
			if environment {
				return nil, nil, fmt.Errorf("GOFLAGS may not contain -args")
			}
			flags = append(flags, "-args")
			flags = append(flags, args[i+1:]...)
			break
		}
		if arg == "--" {
			if environment {
				return nil, nil, fmt.Errorf("GOFLAGS may not contain package arguments")
			}
			roots = append(roots, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if environment {
				return nil, nil, fmt.Errorf("GOFLAGS contains non-flag %q; use -name=value", arg)
			}
			roots = append(roots, arg)
			continue
		}
		name, _, inline := flagName(arg)
		spec, ok := goFlags[name]
		if !ok {
			return nil, nil, fmt.Errorf("unknown Go build/test flag %q; custom test arguments must follow -args", arg)
		}
		if name == "C" && (environment || i != 0) {
			return nil, nil, fmt.Errorf("-C must be the first command-line flag and cannot be set in GOFLAGS")
		}
		flags = append(flags, arg)
		if spec.value && !inline {
			if environment || i+1 == len(args) {
				return nil, nil, fmt.Errorf("-%s requires a value (use -%s=value in GOFLAGS)", name, name)
			}
			i++
			flags = append(flags, args[i])
		}
	}
	for _, root := range roots {
		if root == "" || strings.HasPrefix(root, "-") || strings.ContainsAny(root, "\x00\r\n") {
			return nil, nil, fmt.Errorf("invalid entry package pattern %q", root)
		}
	}
	return flags, roots, nil
}

// ResolveArgs applies -C once, reads GOFLAGS through Go (including GOENV), and
// combines its defaults with command-line overrides. It does not change the
// process directory, environment, or Go configuration.
func ResolveArgs(ctx context.Context, dir string, args []string) (resolvedDir string, flags, roots []string, err error) {
	commandFlags, roots, err := splitFlags(args, false)
	if err != nil {
		return "", nil, nil, err
	}
	if value, ok := Value(commandFlags, "C"); ok {
		if value == "" {
			return "", nil, nil, fmt.Errorf("-C requires a nonempty directory")
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(dir, value)
		}
		dir = value
		commandFlags = Remove(commandFlags, "C", true)
	}
	resolvedDir, err = filepath.Abs(dir)
	if err != nil {
		return "", nil, nil, err
	}
	resolvedDir = canonicalDirectory(resolvedDir)
	c := Command(ctx, resolvedDir, "env", "GOFLAGS")
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := process.Run(ctx, c); err != nil {
		return "", nil, nil, fmt.Errorf("read GOFLAGS: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	defaults, err := splitQuoted(strings.TrimSpace(stdout.String()))
	if err != nil {
		return "", nil, nil, fmt.Errorf("parse GOFLAGS: %w", err)
	}
	defaultFlags, _, err := splitFlags(defaults, true)
	if err != nil {
		return "", nil, nil, err
	}
	flags = append(defaultFlags, commandFlags...)
	flags = Remove(flags, "toolexec", true)
	if err := validateBuildTags(flags); err != nil {
		return "", nil, nil, err
	}
	if len(roots) == 0 {
		roots = []string{"."}
	}
	return resolvedDir, flags, roots, nil
}

func validateBuildTags(flags []string) error {
	if value, _ := Value(flags, "tags"); strings.ContainsAny(value, " '") {
		if _, err := splitQuoted(value); err != nil {
			return fmt.Errorf("invalid -tags value: %w", err)
		}
	}
	for _, tag := range Tags(flags) {
		if tag == "goinject" {
			return fmt.Errorf("goinject is a registration tag; do not enable it in the application build")
		}
	}
	return nil
}

// Tags has Go's last-occurrence-wins semantics, including defaults from GOFLAGS.
func Tags(flags []string) []string {
	value, _ := Value(flags, "tags")
	if strings.ContainsAny(value, " '") {
		tags, _ := splitQuoted(value)
		return tags
	}
	var tags []string
	for _, tag := range strings.Split(value, ",") {
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// Value never treats consumed flag values or the -args tail as flags.
func Value(flags []string, name string) (string, bool) {
	var value string
	var found bool
	for i := 0; i < len(flags); i++ {
		if flags[i] == "-args" || flags[i] == "--args" {
			break
		}
		key, v, inline := flagName(flags[i])
		if !inline {
			v = "true"
			if goFlags[key].value && i+1 < len(flags) {
				i++
				v = flags[i]
			}
		}
		if key == name {
			value, found = v, true
		}
	}
	return value, found
}

func ListFlags(flags []string) []string {
	var out []string
	for i := 0; i < len(flags); i++ {
		if flags[i] == "-args" || flags[i] == "--args" {
			break
		}
		key, _, inline := flagName(flags[i])
		end := i + 1
		if goFlags[key].value && !inline && end < len(flags) {
			end++
		}
		if goFlags[key].list {
			out = append(out, flags[i:end]...)
		}
		i = end - 1
	}
	return out
}

func Has(flags []string, name string) bool {
	value, ok := Value(flags, name)
	if !ok {
		return false
	}
	if !goFlags[name].value && value != "auto" {
		if enabled, err := strconv.ParseBool(value); err == nil {
			return enabled
		}
	}
	return true
}

func Remove(flags []string, name string, value bool) []string {
	var out []string
	for i := 0; i < len(flags); i++ {
		if flags[i] == "-args" || flags[i] == "--args" {
			return append(out, flags[i:]...)
		}
		key, _, inline := flagName(flags[i])
		end := i + 1
		if !inline && (goFlags[key].value || key == name && value) && end < len(flags) {
			end++
		}
		if key != name {
			out = append(out, flags[i:end]...)
		}
		i = end - 1
	}
	return out
}

// splitQuoted follows cmd/internal/quoted's GOFLAGS convention: quotes enclose
// a whole field, with no shell expansion or backslash unescaping.
func splitQuoted(text string) ([]string, error) {
	var out []string
	for text = strings.TrimLeft(text, " \t\r\n"); text != ""; text = strings.TrimLeft(text, " \t\r\n") {
		if text[0] == '\'' || text[0] == '"' {
			quote := text[0]
			end := strings.IndexByte(text[1:], quote)
			if end < 0 {
				return nil, fmt.Errorf("unterminated %c string", quote)
			}
			out = append(out, text[1:end+1])
			text = text[end+2:]
			continue
		}
		end := strings.IndexAny(text, " \t\r\n")
		if end < 0 {
			out = append(out, text)
			break
		}
		out = append(out, text[:end])
		text = text[end:]
	}
	return out, nil
}

func inheritedFlags(value string) []string {
	if value == "" {
		value = os.Getenv("GOFLAGS")
	}
	flags, _ := splitQuoted(value)
	return flags
}
