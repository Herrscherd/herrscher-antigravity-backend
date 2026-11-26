//go:build unix

package antigravity

import (
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	signedInModels = `[ "$1" = models ] && { printf 'gemini-3.8-flash-low\tGemini 3.8 Flash (Low)\n'; exit 0; }`
	answeredTurn   = `cat > /dev/null; printf '{"event":"result","result":{"conversation_id":"c","status":"SUCCESS","response":"OK"}}\n'`
)

func probeWith(t *testing.T, script string, deep bool) contracts.ProbeReport {
	t.Helper()
	stub := writeStubScript(t, script)
	return probe(t.Context(), contracts.ProbeAccess{Config: contracts.PluginConfig{Settings: map[string]string{"cmd": stub}}, Deep: deep})
}

func TestProbeSignedInIsConnected(t *testing.T) {
	report := probeWith(t, signedInModels, false)
	if report.State != contracts.HealthOK || report.Detail.Code != contracts.CodeHealthConnected {
		t.Fatalf("report = %+v", report)
	}
}

func TestProbeSignedOutNamesTheLoginCommand(t *testing.T) {
	report := probeWith(t, `echo "Error: Please sign in to view available models." >&2; exit 1`, false)
	if report.State != contracts.HealthOff || report.Detail.Code != contracts.CodeHealthNotLoggedIn || report.Remedy.Code != contracts.CodeHealthRemedyRun {
		t.Fatalf("report = %+v", report)
	}
}

func TestProbeEmptyListingIsDegraded(t *testing.T) {
	report := probeWith(t, `echo "Fetching available models..."`, false)
	if report.State != contracts.HealthDegraded {
		t.Fatalf("report = %+v", report)
	}
}

func TestProbeMissingBinaryIsOff(t *testing.T) {
	report := probe(t.Context(), contracts.ProbeAccess{Config: contracts.PluginConfig{Settings: map[string]string{"cmd": filepath.Join(t.TempDir(), "absent")}}})
	if report.State != contracts.HealthOff {
		t.Fatalf("report = %+v", report)
	}
}

func TestProbeRefusesProfiles(t *testing.T) {
	report := probe(t.Context(), contracts.ProbeAccess{Account: "work"})
	if report.State != contracts.HealthOff {
		t.Fatalf("report = %+v", report)
	}
}

func TestDeepProbeRunsAPlanTurn(t *testing.T) {
	report := probeWith(t, signedInModels+"\n"+`case " $* " in *" --mode plan "*) ;; *) exit 4;; esac`+"\n"+answeredTurn, true)
	if report.State != contracts.HealthOK {
		t.Fatalf("report = %+v", report)
	}
}

func TestDeepProbeFailedTurnIsDown(t *testing.T) {
	report := probeWith(t, signedInModels+"\n"+`cat > /dev/null; printf '{"event":"result","result":{"status":"ERROR","error":"boom"}}\n'; exit 1`, true)
	if report.State != contracts.HealthDown {
		t.Fatalf("report = %+v", report)
	}
}

func TestDeepProbeRefusedSignInNamesTheLoginCommand(t *testing.T) {
	report := probeWith(t, signedInModels+"\n"+`cat > /dev/null; printf '{"event":"result","result":{"status":"ERROR","error":"authentication failed or timed out"}}\n'; echo "error: authentication failed or timed out" >&2; exit 1`, true)
	if report.State != contracts.HealthDown || report.Detail.Code != contracts.CodeHealthCredentialsRefused || report.Remedy.Code != contracts.CodeHealthRemedyRun {
		t.Fatalf("report = %+v", report)
	}
}

func TestDeepProbeQuotaIsRateLimited(t *testing.T) {
	report := probeWith(t, signedInModels+"\n"+`cat > /dev/null; echo "quota exhausted" >&2; exit 1`, true)
	if report.State != contracts.HealthDegraded || report.Detail.Code != contracts.CodeHealthRateLimited {
		t.Fatalf("report = %+v", report)
	}
}

func TestDeepProbeMissingDirectoryIsDown(t *testing.T) {
	stub := writeStubScript(t, signedInModels)
	settings := map[string]string{"cmd": stub, "dir": filepath.Join(t.TempDir(), "absent")}
	report := probe(t.Context(), contracts.ProbeAccess{Config: contracts.PluginConfig{Settings: settings}, Deep: true})
	if report.State != contracts.HealthDown {
		t.Fatalf("report = %+v", report)
	}
}
