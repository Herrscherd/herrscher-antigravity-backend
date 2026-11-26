package antigravity

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	maxOpenToolCalls  = 256
	maxSeenToolCalls  = 4096
	maxToolNameBytes  = 256
	maxToolErrorRunes = 200
	maxSummaryRunes   = 500
	runCommandTool    = "run_command"
	unnamedTool       = "tool"
)

type stepState int

const (
	stepActive stepState = iota + 1
	stepDone
	stepFailed
	stepCanceled
)

func parseStepState(text string) (stepState, bool) {
	switch strings.ToUpper(strings.TrimSpace(text)) {
	case "ACTIVE":
		return stepActive, true
	case "DONE":
		return stepDone, true
	case "ERROR":
		return stepFailed, true
	case "CANCELED", "CANCELLED":
		return stepCanceled, true
	}
	return 0, false
}

var commandExitCode = regexp.MustCompile(`(?m)^Command exited with code (-?\d+)\.`)

type toolCallTracker struct {
	open      map[string]string
	seen      map[string]struct{}
	openBound boundWarning
	seenBound boundWarning
}

type boundWarning struct {
	reported bool
}

func (w *boundWarning) warnOnce(bound string, limit int) {
	if w.reported {
		return
	}
	w.reported = true
	slog.Warn("tool calls reach a bound of the turn, later starts carry no lifecycle", "bound", bound, "limit", limit)
}

func (t *toolCallTracker) isKnown(callID string) bool {
	_, known := t.seen[callID]
	return known
}

func (t *toolCallTracker) isOpen(callID string) bool {
	_, open := t.open[callID]
	return open
}

func (t *toolCallTracker) begin(callID, tool string) bool {
	if len(t.seen) >= maxSeenToolCalls {
		t.seenBound.warnOnce("max_seen", maxSeenToolCalls)
		return false
	}
	if t.seen == nil {
		t.seen = make(map[string]struct{})
		t.open = make(map[string]string)
	}
	t.seen[callID] = struct{}{}
	if len(t.open) >= maxOpenToolCalls {
		t.openBound.warnOnce("max_open", maxOpenToolCalls)
		return false
	}
	t.open[callID] = tool
	return true
}

func (t *toolCallTracker) finish(callID string) (string, bool) {
	tool, open := t.open[callID]
	delete(t.open, callID)
	return tool, open
}

func toolCallID(stepIndex int) string {
	return "step-" + strconv.Itoa(stepIndex)
}

func toolName(step wireStep) string {
	name := strings.TrimSpace(step.ToolName)
	if name == "" && step.ToolInfo != nil {
		name = strings.TrimSpace(step.ToolInfo.Name)
	}
	if name == "" || len(name) > maxToolNameBytes || firstLine(name) != name {
		return unnamedTool
	}
	return name
}

func toolSummary(info *wireTool) string {
	if info == nil {
		return ""
	}
	for _, key := range []string{"CommandLine", "AbsolutePath", "Path", "TargetFile", "File", "DirectoryPath", "SearchPath", "Query", "Pattern", "Url", "URL", "Description"} {
		var value string
		if json.Unmarshal(info.Parameters[key], &value) == nil && strings.TrimSpace(value) != "" {
			return boundRunes(firstLine(value), maxSummaryRunes)
		}
	}
	return ""
}

type toolResult struct {
	state  contracts.ToolCallState
	reason string
}

func toolOutcome(phase stepState, info *wireTool) toolResult {
	switch phase {
	case stepActive, stepDone:
		return doneOutcome(info)
	case stepFailed:
		return toolResult{state: contracts.ToolCallFailed, reason: orDefault(errorReason(info), "error")}
	case stepCanceled:
		return toolResult{state: contracts.ToolCallFailed, reason: "canceled"}
	}
	return toolResult{state: contracts.ToolCallFailed, reason: "unknown state"}
}

func doneOutcome(info *wireTool) toolResult {
	if info == nil || info.Name != runCommandTool {
		return toolResult{state: contracts.ToolCallSucceeded}
	}
	match := commandExitCode.FindStringSubmatch(info.Output)
	if len(match) < 2 || match[1] == "0" {
		return toolResult{state: contracts.ToolCallSucceeded}
	}
	return toolResult{state: contracts.ToolCallFailed, reason: "exit code " + match[1]}
}

func errorReason(info *wireTool) string {
	if info == nil || info.Error == nil {
		return ""
	}
	return boundRunes(firstLine(orDefault(info.Error.Message, info.Error.Type)), maxToolErrorRunes)
}

func orDefault(text, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	return text
}

func firstLine(text string) string {
	line := strings.TrimSpace(text)
	if end := strings.IndexAny(line, "\r\n"); end >= 0 {
		line = line[:end]
	}
	return strings.TrimSpace(line)
}

func boundRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
