package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const compilerSourceMarker = "actual-compiler-input-not-a-planning-stub"

func performanceFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, map[string]string{
		"main.go": `package main
import("fmt";"testing";"example.test/app/lib")
const sourceMarker = "` + compilerSourceMarker + `"
var sink int
func main(){
 baseline:=testing.AllocsPerRun(1000,func(){sink=lib.Baseline(41)})
 injected:=testing.AllocsPerRun(1000,func(){sink=lib.Subject(41)})
 fmt.Printf("baseline=%g injected=%g value=%d\n",baseline,injected,sink)
}
`,
		"inject.go": registration,
		"lib/lib.go": `package lib
//go:noinline
func Baseline(value int)int{return value+1}
//go:noinline
func Subject(value int)int{return value+1}
`,
		"hooks/noop.go": `//inject:example.test/app/lib/lib.go
package hooks
func Subject(value int)(out int){return 0}
`,
		// This creates a main planning stub during preparation. Inspection must
		// nevertheless return the real compiler's main.go, including its marker.
		"hooks/startup.go": `//inject:main
package hooks
//inject:add
func init(){}
`,
	})
	f.env["GOCACHE"] = filepath.Join(f.dir, "go-build-cache")
	return f
}

type observedSnapshot struct {
	directory, cache, fingerprint string
	compiledRecords               int
}

type observedRecord struct {
	Package  string
	Planning bool
	Sources  []struct {
		Path string
		Data []byte
	}
	Result struct {
		Matches []struct{ Function string }
	}
}

func observeSnapshot(t *testing.T, f *fixture, output string) observedSnapshot {
	t.Helper()
	basic := f.inspect(output) // also registers cleanup for this retained session
	var result observedSnapshot
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "GOINJECT_WORK=") {
			result.directory = strings.TrimSpace(strings.TrimPrefix(line, "GOINJECT_WORK="))
		}
	}
	var session struct {
		Cache, Fingerprint string
		Root               struct{ Dir string }
	}
	data, err := os.ReadFile(filepath.Join(result.directory, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	userCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	decoded, decodeErr := hex.DecodeString(session.Fingerprint)
	actualRoot, rootErr := os.Stat(session.Root.Dir)
	fixtureRoot, fixtureErr := os.Stat(f.dir)
	if decodeErr != nil || len(decoded) != sha256.Size || session.Fingerprint != basic.Fingerprint || rootErr != nil || fixtureErr != nil || !os.SameFile(actualRoot, fixtureRoot) {
		t.Fatal("session ownership or fingerprint did not match this fixture")
	}
	expectedCache := filepath.Join(userCache, "go-inject", "v1", session.Fingerprint)
	if filepath.Clean(session.Cache) != filepath.Clean(expectedCache) {
		t.Fatalf("refusing to inspect or damage cache outside this fixture's verified fingerprint directory: %s", session.Cache)
	}
	result.cache, result.fingerprint = expectedCache, session.Fingerprint
	// This unique fingerprint includes the temporary fixture root. Never delete
	// the shared cache parent or touch another build's source snapshot.
	t.Cleanup(func() {
		if err := os.RemoveAll(expectedCache); err != nil {
			t.Errorf("clean fixture source snapshot: %v", err)
		}
	})
	entries, err := os.ReadDir(filepath.Join(result.directory, "records"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			result.compiledRecords++
		}
	}
	var report struct {
		Packages []observedRecord
	}
	if err := json.Unmarshal([]byte(f.cli("inspect", "--json", result.directory)), &report); err != nil {
		t.Fatal(err)
	}
	var mainSource, matchedNoop bool
	for _, record := range report.Packages {
		if record.Planning || record.Package == "main" {
			t.Fatalf("inspect returned a preflight record instead of actual compiler input: %+v", record)
		}
		if record.Package == "example.test/app" {
			for _, source := range record.Sources {
				if filepath.Base(source.Path) == "goinject_entry.go" {
					t.Fatal("inspect returned the synthetic main planning source")
				}
				mainSource = mainSource || filepath.Base(source.Path) == "main.go" && strings.Contains(string(source.Data), compilerSourceMarker)
			}
		}
		if record.Package == "example.test/app/lib" {
			for _, match := range record.Result.Matches {
				matchedNoop = matchedNoop || match.Function == "Subject"
			}
		}
	}
	if !mainSource || !matchedNoop {
		t.Fatalf("inspect lacks real main source or applied no-op match: main=%t noop=%t", mainSource, matchedNoop)
	}
	return result
}

func logBuild(t *testing.T, f *fixture, label, binary string) observedSnapshot {
	t.Helper()
	started := time.Now()
	output := f.cli("build", "-work", "-o", binary, ".")
	elapsed := time.Since(started)
	snapshot := observeSnapshot(t, f, output)
	t.Logf("build baseline: phase=%s go=%s target=%s/%s elapsed=%s compiled_records=%d fingerprint=%s", label, runtime.Version(), runtime.GOOS, runtime.GOARCH, elapsed, snapshot.compiledRecords, snapshot.fingerprint)
	return snapshot
}

func assertAllocationBaseline(t *testing.T, f *fixture, binary string) {
	t.Helper()
	output, err := command(f.dir, f.env, 30*time.Second, filepath.Join(f.dir, binary))
	if err != nil {
		t.Fatalf("allocation measurement failed: %v\n%s", err, output)
	}
	var baseline, injected float64
	var value int
	if count, err := fmt.Sscanf(strings.TrimSpace(output), "baseline=%f injected=%f value=%d", &baseline, &injected, &value); err != nil || count != 3 {
		t.Fatalf("invalid allocation measurements %q: %v", output, err)
	}
	t.Logf("allocation baseline: runs=1000 baseline_allocs=%g injected_allocs=%g value=%d", baseline, injected, value)
	if value != 42 || injected > baseline {
		t.Fatalf("no-op template changed the value or increased allocations: %s", output)
	}
}

func TestNoopTemplateAllocationsAndBuildTimingBaseline(t *testing.T) {
	f := performanceFixture(t)
	binary := "performance-app" + exeSuffix()
	cold := logBuild(t, f, "cold", binary)
	if cold.compiledRecords == 0 {
		t.Fatal("cold build did not capture actual compile records")
	}
	assertAllocationBaseline(t, f, binary)
	hot := logBuild(t, f, "hot", binary)
	if cold.fingerprint != hot.fingerprint || hot.compiledRecords != 0 {
		t.Fatalf("identical warm build did not reuse compiled inputs: cold=%+v hot=%+v", cold, hot)
	}
	assertAllocationBaseline(t, f, binary)
	// Timing is an observed baseline, not an arbitrary machine-speed threshold.
}

func TestSourceSnapshotRecoversAfterIndependentEviction(t *testing.T) {
	for _, damage := range []string{"missing-main-record", "corrupt-main-record"} {
		t.Run(damage, func(t *testing.T) {
			f := performanceFixture(t)
			binary := "snapshot-app" + exeSuffix()
			initial := logBuild(t, f, "initial", binary)
			warm := logBuild(t, f, "warm-before-damage", binary)
			if initial.fingerprint != warm.fingerprint || warm.compiledRecords != 0 {
				t.Fatal("could not establish warm Go cache before independently damaging the source snapshot")
			}
			filename := mainSnapshotRecord(t, warm.cache)
			if damage == "missing-main-record" {
				if err := os.Remove(filename); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(filename, []byte("{corrupted-source-snapshot"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// GOCACHE remains intact; only the independent source snapshot is damaged.
			recovered := logBuild(t, f, "recovery-"+damage, binary)
			if recovered.fingerprint != initial.fingerprint || recovered.compiledRecords == 0 {
				t.Fatalf("damaged snapshot was not rebuilt under the same input identity: %+v", recovered)
			}
			assertAllocationBaseline(t, f, binary)
			verifySnapshotDigest(t, recovered.cache, filepath.Base(filename))
			hot := logBuild(t, f, "warm-after-recovery", binary)
			if hot.fingerprint != recovered.fingerprint || hot.compiledRecords != 0 {
				t.Fatalf("recovery kept forcing recompilation instead of repairing the snapshot once: %+v", hot)
			}
		})
	}
}

func mainSnapshotRecord(t *testing.T, cache string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cache, "records"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		filename := filepath.Join(cache, "records", entry.Name())
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		var record observedRecord
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		if record.Package == "example.test/app" && !record.Planning {
			verifySnapshotDigest(t, cache, entry.Name())
			return filename
		}
	}
	t.Fatal("no actual main snapshot to damage")
	return ""
}

func verifySnapshotDigest(t *testing.T, cache, name string) {
	t.Helper()
	if filepath.Base(name) != name {
		t.Fatal("snapshot filename escapes records directory")
	}
	data, err := os.ReadFile(filepath.Join(cache, "complete.json"))
	if err != nil {
		t.Fatal(err)
	}
	var complete map[string]string
	if err := json.Unmarshal(data, &complete); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(cache, "records", name))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if complete[name] != hex.EncodeToString(digest[:]) {
		t.Fatal("recovery did not restore the completion manifest's content digest")
	}
}
