package antigravity

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func replay(t *testing.T, stream, resumed string) (turnResult, []contracts.BackendEvent, error) {
	t.Helper()
	var events []contracts.BackendEvent
	turn, err := readTurn(bufio.NewReader(strings.NewReader(stream)), resumed, func(event contracts.BackendEvent) {
		events = append(events, event)
	})
	return turn, events, err
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func eventsOfKind(events []contracts.BackendEvent, kind string) []contracts.BackendEvent {
	var kept []contracts.BackendEvent
	for index := range events {
		if events[index].Kind == kind {
			kept = append(kept, events[index])
		}
	}
	return kept
}

func TestRecordedToolTurnFollowsTheCallAndKeepsTheConversation(t *testing.T) {
	turn, events, err := replay(t, fixture(t, "read_file.ndjson"), "")
	if err != nil {
		t.Fatal(err)
	}
	if turn.Text != "hello\n" || turn.Conversation != "a9d56710-40b3-49cc-bdd2-07b9b47162ba" || turn.Failure != "" {
		t.Fatalf("turn = %+v", turn)
	}
	tools := eventsOfKind(events, contracts.BackendKindTool)
	if len(tools) != 1 || tools[0].Tool != "read_file" || tools[0].Detail != "/work/a.txt" || tools[0].ToolCall.State != contracts.ToolCallStarted {
		t.Fatalf("tool starts = %+v", tools)
	}
	done := eventsOfKind(events, contracts.BackendKindToolDone)
	if len(done) != 1 || done[0].ToolCall.ID != tools[0].ToolCall.ID || done[0].ToolCall.State != contracts.ToolCallSucceeded {
		t.Fatalf("tool results = %+v", done)
	}
	texts := eventsOfKind(events, "text")
	if len(texts) != 1 || texts[0].Detail != "hello\n" {
		t.Fatalf("texts = %+v", texts)
	}
}

func TestRecordedTurnReportsUsagePerStepAndTheTurnTotal(t *testing.T) {
	_, events, err := replay(t, fixture(t, "read_file.ndjson"), "")
	if err != nil {
		t.Fatal(err)
	}
	usage := eventsOfKind(events, "usage")
	if len(usage) != 2 || usage[0].OutTokens != 201 || usage[1].OutTokens != 41 || usage[1].CacheRead != 9004 {
		t.Fatalf("usage = %+v", usage)
	}
	results := eventsOfKind(events, "result")
	if len(results) != 1 || results[0].IsError || results[0].OutTokens != 242 || results[0].InTokens != 13393 || results[0].CacheRead != 9004 {
		t.Fatalf("result = %+v", results)
	}
}

func TestResumedTurnCountsOnlyItsOwnUsage(t *testing.T) {
	_, events, err := replay(t, fixture(t, "resumed.ndjson"), "a9d56710-40b3-49cc-bdd2-07b9b47162ba")
	if err != nil {
		t.Fatal(err)
	}
	results := eventsOfKind(events, "result")
	if len(results) != 1 || results[0].OutTokens != 65 || results[0].InTokens != 2304 || results[0].CacheRead != 9295 {
		t.Fatalf("result = %+v, want the usage of this turn, not the conversation total", results)
	}
	if notices := eventsOfKind(events, contracts.BackendKindNotice); len(notices) != 0 {
		t.Fatalf("notices = %+v, want none for a conversation that was resumed", notices)
	}
}

func TestResumeThatOpensAnotherConversationIsReported(t *testing.T) {
	turn, events, err := replay(t, fixture(t, "resumed.ndjson"), "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	notices := eventsOfKind(events, contracts.BackendKindNotice)
	if len(notices) != 1 || notices[0].Notice.Code != contracts.CodeResumeLost {
		t.Fatalf("notices = %+v, want one resume lost", notices)
	}
	if turn.Conversation != "a9d56710-40b3-49cc-bdd2-07b9b47162ba" {
		t.Fatalf("conversation = %q, want the one agy opened", turn.Conversation)
	}
}

func TestRecordedToolFailuresCarryTheirReason(t *testing.T) {
	_, events, err := replay(t, fixture(t, "tool_failures.ndjson"), "")
	if err != nil {
		t.Fatal(err)
	}
	done := eventsOfKind(events, contracts.BackendKindToolDone)
	if len(done) != 2 {
		t.Fatalf("tool results = %+v", done)
	}
	if done[0].ToolCall.Tool != "run_command" || done[0].ToolCall.State != contracts.ToolCallSucceeded {
		t.Fatalf("command result = %+v, want success for an exit code 0", done[0].ToolCall)
	}
	if done[1].ToolCall.State != contracts.ToolCallFailed || done[1].ToolCall.Error != "file does not exist" {
		t.Fatalf("read result = %+v", done[1].ToolCall)
	}
}

func TestStreamedDeltasMakeOneTextBlock(t *testing.T) {
	turn, events, err := replay(t, fixture(t, "tool_failures.ndjson"), "")
	if err != nil {
		t.Fatal(err)
	}
	texts := eventsOfKind(events, "text")
	if len(texts) != 1 || texts[0].Detail != turn.Text {
		t.Fatalf("texts = %+v, want one block equal to the response %q", texts, turn.Text)
	}
}

func TestErrorResultFailsTheTurnWithItsMessage(t *testing.T) {
	turn, events, err := replay(t, fixture(t, "unknown_model.ndjson"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(turn.Failure, "invalid model selection") {
		t.Fatalf("failure = %q", turn.Failure)
	}
	if results := eventsOfKind(events, "result"); len(results) != 1 || !results[0].IsError {
		t.Fatalf("result = %+v", results)
	}
}

func TestCommandWithNonZeroExitFails(t *testing.T) {
	stream := `{"event":"step_update","step_update":{"step_index":2,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"false"},"output":"Command exited with code 1.\n"}}}
{"event":"result","result":{"conversation_id":"c","status":"SUCCESS","response":"done"}}
`
	_, events, err := replay(t, stream, "")
	if err != nil {
		t.Fatal(err)
	}
	starts := eventsOfKind(events, contracts.BackendKindTool)
	done := eventsOfKind(events, contracts.BackendKindToolDone)
	if len(starts) != 1 || starts[0].Detail != "false" {
		t.Fatalf("starts = %+v, want the call opened from its only update", starts)
	}
	if len(done) != 1 || done[0].ToolCall.State != contracts.ToolCallFailed || done[0].ToolCall.Error != "exit code 1" {
		t.Fatalf("results = %+v", done)
	}
}

func TestRepeatedToolUpdateClosesTheCallOnce(t *testing.T) {
	update := `{"event":"step_update","step_update":{"step_index":3,"state":"DONE","step_type":"tool","tool_name":"list_dir"}}` + "\n"
	stream := update + update + `{"event":"result","result":{"status":"SUCCESS"}}` + "\n"
	_, events, err := replay(t, stream, "")
	if err != nil {
		t.Fatal(err)
	}
	if starts, done := eventsOfKind(events, contracts.BackendKindTool), eventsOfKind(events, contracts.BackendKindToolDone); len(starts) != 1 || len(done) != 1 {
		t.Fatalf("starts = %d, results = %d, want one of each", len(starts), len(done))
	}
}

func TestStreamWithoutResultIsAnError(t *testing.T) {
	stream := `{"event":"init","conversation_id":"conv-1"}` + "\nnot json\n"
	turn, _, err := replay(t, stream, "")
	ended, isEnded := errors.AsType[*StreamEndedError](err)
	if !isEnded || ended.Unparsable != 1 || !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want a stream ended error after one unparsable line", err)
	}
	if turn.Conversation != "conv-1" {
		t.Fatalf("conversation = %q, want the one init announced", turn.Conversation)
	}
}

func TestOverlongLineStopsTheRead(t *testing.T) {
	stream := strings.Repeat("x", maxStreamLine+1) + "\n"
	_, _, err := replay(t, stream, "")
	if _, isTooLong := errors.AsType[*LineTooLongError](err); !isTooLong {
		t.Fatalf("err = %v, want a line too long error", err)
	}
}
