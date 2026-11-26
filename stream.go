package antigravity

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	maxStreamLine      = 16 << 20
	maxPendingText     = 16 << 20
	textBlockSeparator = "\n\n"
	statusSuccess      = "SUCCESS"
)

const (
	eventInit       = "init"
	eventStepUpdate = "step_update"
	eventResult     = "result"
)

const (
	stepAgentResponse = "agent_response"
	stepTool          = "tool"
)

type wireEvent struct {
	Event          string      `json:"event"`
	ConversationID string      `json:"conversation_id"`
	StepUpdate     *wireStep   `json:"step_update"`
	Result         *wireResult `json:"result"`
}

type wireStep struct {
	StepIndex int        `json:"step_index"`
	State     string     `json:"state"`
	StepType  string     `json:"step_type"`
	TextDelta string     `json:"text_delta"`
	ToolName  string     `json:"tool_name"`
	ToolInfo  *wireTool  `json:"tool_info"`
	Usage     *wireUsage `json:"usage"`
}

type wireTool struct {
	Name       string                     `json:"name"`
	Parameters map[string]json.RawMessage `json:"parameters"`
	Output     string                     `json:"output"`
	Error      *wireToolError             `json:"error"`
}

type wireToolError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type wireUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	CacheReadTokens int `json:"cache_read_tokens"`
}

type wireResult struct {
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"`
	Response       string `json:"response"`
	Error          string `json:"error"`
}

// LineTooLongError reports a line of the stream longer than the reader accepts.
type LineTooLongError struct {
	Limit int
}

func (e *LineTooLongError) Error() string {
	return fmt.Sprintf("agy stream line is longer than %d bytes", e.Limit)
}

// StreamEndedError reports that the stream ended before its result event.
// Unparsable counts the non-empty lines that were not JSON, and Cause is the
// read error that ended the stream.
type StreamEndedError struct {
	Unparsable int
	Cause      error
}

func (e *StreamEndedError) Error() string {
	return fmt.Sprintf("agy stream ended after %d unparsable line(s): %v", e.Unparsable, e.Cause)
}

func (e *StreamEndedError) Unwrap() error {
	return e.Cause
}

type turnResult struct {
	Text         string
	Conversation string
	Failure      string
}

type turnState struct {
	resumed      string
	conversation string
	emitter      eventEmitter
	text         textBlocks
	tools        toolCallTracker
	usage        turnUsage
}

type eventEmitter struct {
	onEvent func(contracts.BackendEvent)
}

func (e eventEmitter) emit(event contracts.BackendEvent) {
	if e.onEvent != nil {
		e.onEvent(event)
	}
}

type turnUsage struct {
	output int
	last   wireUsage
}

func (u *turnUsage) add(usage wireUsage) {
	u.output += max(usage.OutputTokens, 0)
	u.last = usage
}

func readTurn(reader *bufio.Reader, resumed string, onEvent func(contracts.BackendEvent)) (turnResult, error) {
	state := turnState{resumed: resumed, emitter: eventEmitter{onEvent: onEvent}}
	var skipped int
	for {
		line, err := readLine(reader)
		if _, isTooLong := errors.AsType[*LineTooLongError](err); isTooLong {
			return turnResult{}, err
		}
		if len(bytes.TrimSpace(line)) > 0 {
			var event wireEvent
			if json.Unmarshal(line, &event) != nil {
				skipped++
			} else if turn, isDone := state.apply(event); isDone {
				return turn, nil
			}
		}
		if err != nil {
			return turnResult{Conversation: state.conversation}, &StreamEndedError{Unparsable: skipped, Cause: err}
		}
	}
}

func readLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(line)+len(chunk) > maxStreamLine {
			return nil, &LineTooLongError{Limit: maxStreamLine}
		}
		line = append(line, chunk...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}

func (s *turnState) apply(event wireEvent) (turnResult, bool) {
	switch event.Event {
	case eventInit:
		s.start(event.ConversationID)
	case eventStepUpdate:
		if event.StepUpdate != nil {
			s.step(*event.StepUpdate)
		}
	case eventResult:
		if event.Result != nil {
			return s.finish(*event.Result), true
		}
	}
	return turnResult{}, false
}

func (s *turnState) start(conversation string) {
	if conversation == "" {
		return
	}
	if s.resumed != "" && conversation != s.resumed && s.conversation == "" {
		s.emitter.emit(contracts.BackendEvent{Kind: contracts.BackendKindNotice, Notice: &contracts.Notice{Code: contracts.CodeResumeLost}})
	}
	s.conversation = conversation
}

func (s *turnState) step(step wireStep) {
	phase, known := parseStepState(step.State)
	if !known {
		return
	}
	switch step.StepType {
	case stepAgentResponse:
		s.response(step, phase)
	case stepTool:
		s.tool(step, phase)
	}
}

func (s *turnState) response(step wireStep, phase stepState) {
	if block, full := s.text.add(step.StepIndex, step.TextDelta); full {
		s.emitText(block)
	}
	if phase == stepActive {
		return
	}
	s.emitText(s.text.flush())
	if step.Usage != nil {
		s.usage.add(*step.Usage)
		s.emitter.emit(contracts.BackendEvent{Kind: "usage", InTokens: step.Usage.InputTokens, OutTokens: step.Usage.OutputTokens, CacheRead: step.Usage.CacheReadTokens})
	}
}

func (s *turnState) emitText(block string) {
	if strings.TrimSpace(block) == "" {
		return
	}
	s.emitter.emit(contracts.BackendEvent{Kind: "text", Detail: s.text.detail(block)})
}

func (s *turnState) tool(step wireStep, phase stepState) {
	name := toolName(step)
	callID := toolCallID(step.StepIndex)
	if phase == stepActive {
		s.startTool(callID, name, step.ToolInfo)
		return
	}
	if !s.tools.isOpen(callID) {
		s.startTool(callID, name, step.ToolInfo)
	}
	tool, open := s.tools.finish(callID)
	if !open {
		return
	}
	outcome := toolOutcome(phase, step.ToolInfo)
	s.emitter.emit(contracts.BackendEvent{Kind: contracts.BackendKindToolDone, ToolCall: &contracts.ToolCall{ID: callID, Tool: tool, State: outcome.state, Error: outcome.reason}})
}

func (s *turnState) startTool(callID, name string, info *wireTool) {
	if s.tools.isKnown(callID) {
		return
	}
	detail := toolSummary(info)
	started := contracts.BackendEvent{Kind: contracts.BackendKindTool, Tool: name, Detail: detail}
	if s.tools.begin(callID, name) {
		started.ToolCall = &contracts.ToolCall{ID: callID, Tool: name, Detail: detail, State: contracts.ToolCallStarted}
	}
	s.emitter.emit(started)
}

func (s *turnState) finish(result wireResult) turnResult {
	s.emitText(s.text.flush())
	if result.ConversationID != "" {
		s.start(result.ConversationID)
	}
	turn := turnResult{Text: result.Response, Conversation: s.conversation}
	failed := result.Status != statusSuccess
	if failed {
		turn.Failure = failureMessage(result)
	}
	s.emitter.emit(contracts.BackendEvent{
		Kind:      "result",
		IsError:   failed,
		OutTokens: s.usage.output,
		InTokens:  s.usage.last.InputTokens,
		CacheRead: s.usage.last.CacheReadTokens,
	})
	return turn
}

func failureMessage(result wireResult) string {
	if message := strings.TrimSpace(result.Error); message != "" {
		return message
	}
	if result.Status == "" {
		return "result without a status"
	}
	return "status " + result.Status
}

type textBlocks struct {
	index   int
	parts   []string
	size    int
	emitted bool
}

func (t *textBlocks) add(index int, delta string) (string, bool) {
	var full string
	if t.size > 0 && index != t.index {
		full = t.flush()
	}
	t.index = index
	if t.size+len(delta) > maxPendingText {
		full += t.flush()
	}
	t.parts = append(t.parts, delta)
	t.size += len(delta)
	return full, full != ""
}

func (t *textBlocks) flush() string {
	block := strings.Join(t.parts, "")
	t.parts = nil
	t.size = 0
	return block
}

func (t *textBlocks) detail(block string) string {
	if !t.emitted {
		t.emitted = true
		return block
	}
	return textBlockSeparator + block
}
