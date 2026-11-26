package antigravity

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const recordedModels = "Fetching available models...\n" +
	"gemini-4-hydrogen-high\tGemini 4 Hydrogen (High)\n" +
	"gemini-3.8-flash-low\tGemini 3.8 Flash (Low)\n" +
	"claude-opus-4-6-thinking\tClaude Opus 4.6 (Thinking)\n"

func envWith(cmd string) func(string) string {
	return func(key string) string {
		if key == envCmd {
			return cmd
		}
		return ""
	}
}

func nativeAccess(cmd string) contracts.DiscoveryAccess {
	return contracts.DiscoveryAccess{Getenv: envWith(cmd), Policy: contracts.PolicyAll}
}

func TestRecordedModelListingIsParsed(t *testing.T) {
	specs := parseModelsOutput(recordedModels)
	if len(specs) != 3 {
		t.Fatalf("specs = %+v, want the three listed models", specs)
	}
	if specs[0].ID != "gemini-4-hydrogen-high" || specs[0].Arg != specs[0].ID || specs[0].Label != "Gemini 4 Hydrogen (High)" || specs[0].Route != contracts.RouteNative {
		t.Fatalf("first spec = %+v", specs[0])
	}
	if err := contracts.ValidateModels("antigravity", specs); err != nil {
		t.Fatal(err)
	}
}

func TestModelLinesThatAreNotListingsAreSkipped(t *testing.T) {
	for _, line := range []string{"", "Fetching available models...", "Bad ID\tLabel", "model-a\t", "model-b\tLabel\x1b[0m", "-lead\tLabel"} {
		if spec, ok := parseModelLine(line); ok {
			t.Fatalf("line %q parsed as %+v", line, spec)
		}
	}
}

func TestDuplicateModelIsListedOnce(t *testing.T) {
	specs := parseModelsOutput("model-a\tA\nmodel-a\tA again\n")
	if len(specs) != 1 || specs[0].Label != "A" {
		t.Fatalf("specs = %+v", specs)
	}
}

func TestDiscoverGatewayOnlyNeverRunsTheCLI(t *testing.T) {
	access := contracts.DiscoveryAccess{Getenv: envWith(filepath.Join(t.TempDir(), "absent")), Policy: contracts.PolicyGatewayOnly}
	specs, err := discover(t.Context(), access)
	if err != nil || specs != nil {
		t.Fatalf("specs = %+v, err = %v", specs, err)
	}
}

func TestDiscoverMissingBinaryIsAnError(t *testing.T) {
	_, err := discover(t.Context(), nativeAccess(filepath.Join(t.TempDir(), "absent")))
	if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("err = %v, want the missing binary", err)
	}
}

const fakeHangEnv = "HERRSCHER_FAKE_AGY_HANG"

func TestMain(m *testing.M) {
	if os.Getenv(fakeHangEnv) == "1" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeHangingSelf(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeHangEnv, "1")
	return self
}

func TestDiscoverStopsWhenDeadlinePasses(t *testing.T) {
	command := fakeHangingSelf(t)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := discover(ctx, nativeAccess(command)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("discover took %v past its deadline", elapsed)
	}
}

func TestKillAfterTreeFailureLogsAndKillsProcess(t *testing.T) {
	logged := captureLog(t)
	cmd := exec.CommandContext(t.Context(), fakeHangingSelf(t), "models")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := killAfterTreeFailure(cmd.Process, errors.New("taskkill refused")); err != nil {
		t.Fatal(err)
	}
	if waitErr := cmd.Wait(); waitErr == nil {
		t.Fatal("process survived its kill")
	}
	if !strings.Contains(logged.String(), "taskkill failed") {
		t.Fatalf("fallback kill not logged: %s", logged)
	}
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}
