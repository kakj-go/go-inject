package e2e

import (
	"strings"
	"testing"
)

func TestInternalAndExternalTestPackagesApplyInjectionOnce(t *testing.T) {
	f := newFixture(t, map[string]string{
		"lib/lib.go":    "package lib\nfunc Value()int{return 1}\n",
		"lib/inject.go": "//go:build goinject\n\npackage lib\nimport _ \"example.test/app/hooks\"\n",
		"lib/internal_test.go": `package lib
import "testing"
func TestInternal(t *testing.T){if got:=Value();got!=2{t.Fatalf("internal got %d, want exactly one injection",got)}}
`,
		"lib/external_test.go": `package lib_test
import("testing";"example.test/app/lib")
func TestExternal(t *testing.T){if got:=lib.Value();got!=2{t.Fatalf("external got %d, want exactly one injection",got)}}
`,
		"hooks/hook.go": `//inject:example.test/app/lib/lib.go
package hooks
func Value()(out int){defer func(){out++}();return 0}
`,
	})
	output := f.cli("test", "-count=1", "-v", "./lib")
	if !strings.Contains(output, "PASS: TestInternal") || !strings.Contains(output, "PASS: TestExternal") {
		t.Fatalf("test driver did not execute both package variants:\n%s", output)
	}
}
