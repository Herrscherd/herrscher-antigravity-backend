package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	deepProbePrompt          = "Reply with the single word OK."
	rateLimitText            = "rate limit"
	quotaText                = "quota"
	reasonTimeout            = "timeout"
	reasonCanceled           = "canceled"
	reasonCommandNotFound    = "command not found"
	reasonCommandFailed      = "command failed"
	reasonOutputTooLarge     = "output too large"
	reasonNoModel            = "no model listed"
	reasonTurnFailed         = "turn failed"
	reasonDirMissing         = "working directory missing"
	reasonProfileUnsupported = "profiles not supported"
)

func probe(ctx context.Context, access contracts.ProbeAccess) contracts.ProbeReport {
	if access.Account != "" {
		return unreachableReport(contracts.HealthOff, reasonProfileUnsupported)
	}
	base := probeCommand(access)
	started := time.Now()
	report, isSignedIn := modelsReport(ctx, base)
	if access.Deep && isSignedIn {
		report = deepReport(ctx, base, access.Config, report)
	}
	report.Latency = time.Since(started)
	return report
}

func probeCommand(access contracts.ProbeAccess) []string {
	if configured := access.Config.Get("cmd"); configured != "" {
		return baseCommand(configured)
	}
	if access.Getenv == nil {
		return baseCommand("")
	}
	return baseCommand(access.Getenv(envCmd))
}

func modelsReport(ctx context.Context, base []string) (contracts.ProbeReport, bool) {
	output, err := runBounded(ctx, append(slices.Clone(base), "models"), "", "")
	switch {
	case err != nil && output.isSignedOut():
		return signedOutReport(base), false
	case err != nil:
		return failureReport(ctx, err), false
	case output.stdout.exceeded:
		return unreachableReport(contracts.HealthDown, reasonOutputTooLarge), false
	case len(parseModelsOutput(output.stdout.data.String())) == 0:
		return unreachableReport(contracts.HealthDegraded, reasonNoModel), false
	}
	return connectedReport(), true
}

func signedOutReport(base []string) contracts.ProbeReport {
	return contracts.ProbeReport{
		State:  contracts.HealthOff,
		Detail: contracts.Notice{Code: contracts.CodeHealthNotLoggedIn},
		Remedy: loginRemedy(base),
	}
}

func connectedReport() contracts.ProbeReport {
	return contracts.ProbeReport{State: contracts.HealthOK, Detail: contracts.Notice{Code: contracts.CodeHealthConnected}}
}

func deepReport(ctx context.Context, base []string, config contracts.PluginConfig, signedIn contracts.ProbeReport) contracts.ProbeReport {
	if failed, isFailed := deepFailure(ctx, base, config); isFailed {
		return failed
	}
	return signedIn
}

func deepFailure(ctx context.Context, base []string, config contracts.PluginConfig) (contracts.ProbeReport, bool) {
	dir := config.Get("dir")
	if dir != "" {
		if _, err := os.Stat(dir); err != nil {
			return unreachableReport(contracts.HealthDown, reasonDirMissing), true
		}
	}
	input, err := turnInput(contracts.Prompt{Content: deepProbePrompt})
	if err != nil {
		return unreachableReport(contracts.HealthDown, reasonTurnFailed), true
	}
	argv := append(slices.Clone(base), "--input-format", "stream-json", "--output-format", "stream-json", "--mode", "plan")
	if model := config.Get("model"); model != "" {
		argv = append(argv, "--model", model)
	}
	output, err := runBounded(ctx, argv, dir, input)
	if err == nil && isAnsweredTurn(output) {
		return contracts.ProbeReport{}, false
	}
	switch {
	case output.isSignedOut():
		return contracts.ProbeReport{
			State:  contracts.HealthDown,
			Detail: contracts.Notice{Code: contracts.CodeHealthCredentialsRefused},
			Remedy: loginRemedy(base),
		}, true
	case output.mentions(rateLimitText), output.mentions(quotaText):
		return contracts.ProbeReport{State: contracts.HealthDegraded, Detail: contracts.Notice{Code: contracts.CodeHealthRateLimited}}, true
	case err != nil:
		return failureReport(ctx, err), true
	case output.stdout.exceeded:
		return unreachableReport(contracts.HealthDown, reasonOutputTooLarge), true
	}
	return unreachableReport(contracts.HealthDown, reasonTurnFailed), true
}

func isAnsweredTurn(output *boundedOutput) bool {
	if output.stdout.exceeded {
		return false
	}
	lines := strings.Split(strings.TrimSpace(output.stdout.data.String()), "\n")
	var event wireEvent
	if json.Unmarshal([]byte(lines[len(lines)-1]), &event) != nil || event.Result == nil {
		return false
	}
	return event.Event == eventResult && event.Result.Status == statusSuccess
}

func failureReport(ctx context.Context, err error) contracts.ProbeReport {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return unreachableReport(contracts.HealthDegraded, reasonTimeout)
	case ctx.Err() != nil:
		return unreachableReport(contracts.HealthDegraded, reasonCanceled)
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return unreachableReport(contracts.HealthOff, reasonCommandNotFound)
	}
	if exited, isExit := errors.AsType[*exec.ExitError](err); isExit {
		return unreachableReport(contracts.HealthDown, exited.String())
	}
	return unreachableReport(contracts.HealthDown, reasonCommandFailed)
}

func unreachableReport(state contracts.HealthState, reason string) contracts.ProbeReport {
	return contracts.ProbeReport{
		State:  state,
		Detail: contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", reason)}},
	}
}

func loginRemedy(base []string) contracts.Notice {
	return contracts.Notice{Code: contracts.CodeHealthRemedyRun, Args: []contracts.Arg{contracts.Text("Command", base[0])}}
}
