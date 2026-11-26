//go:build unix

package antigravity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const stubScript = `#!/bin/sh
printf '%s\0' "$@" > "$AGY_STUB_ARGV"
cat > "$AGY_STUB_STDIN"
printf '{"event":"init","conversation_id":"conv-%s"}\n' "$AGY_STUB_TURN"
printf '{"event":"result","result":{"conversation_id":"conv-%s","status":"%s","response":"answer","error":"%s"}}\n' "$AGY_STUB_TURN" "${AGY_STUB_STATUS:-SUCCESS}" "$AGY_STUB_ERROR"
[ "${AGY_STUB_STATUS:-SUCCESS}" = SUCCESS ] || exit 1
`

type stubRun struct {
	argv  []string
	stdin string
}

func writeStubScript(t *testing.T, body string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

func agyStub(t *testing.T) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "agy")
	if err := os.WriteFile(stub, []byte(stubScript), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("AGY_STUB_ARGV", filepath.Join(dir, "argv"))
	t.Setenv("AGY_STUB_STDIN", filepath.Join(dir, "stdin"))
	return stub
}

func lastRun(t *testing.T) stubRun {
	t.Helper()
	argv, err := os.ReadFile(os.Getenv("AGY_STUB_ARGV"))
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := os.ReadFile(os.Getenv("AGY_STUB_STDIN"))
	if err != nil {
		t.Fatal(err)
	}
	return stubRun{argv: strings.Split(strings.TrimSuffix(string(argv), "\x00"), "\x00"), stdin: string(stdin)}
}

func flagValue(argv []string, name string) (string, bool) {
	index := slices.Index(argv, name)
	if index < 0 || index+1 >= len(argv) {
		return "", false
	}
	return argv[index+1], true
}

func TestPromptTravelsOnStdinAsOneInputMessage(t *testing.T) {
	backend := NewBackend(t.Context(), Config{Cmd: agyStub(t), Model: "gemini-3.8-flash-low"})
	t.Setenv("AGY_STUB_TURN", "1")
	prompt := contracts.Prompt{Content: "distinctive prompt", Context: "recalled marker", Attachments: []string{"/tmp/shot.png"}}

	text, err := backend.Respond(t.Context(), prompt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "answer" {
		t.Fatalf("text = %q", text)
	}
	run := lastRun(t)
	for _, secret := range []string{"distinctive prompt", "recalled marker", "/tmp/shot.png"} {
		if strings.Contains(strings.Join(run.argv, " "), secret) {
			t.Fatalf("argv %q carries %q, want the prompt on stdin only", run.argv, secret)
		}
	}
	var message inputMessage
	if err := json.Unmarshal([]byte(run.stdin), &message); err != nil || !strings.HasSuffix(run.stdin, "\n") || strings.Count(run.stdin, "\n") != 1 {
		t.Fatalf("stdin %q is not one NDJSON line: %v", run.stdin, err)
	}
	want := withContext(prompt.Context, withAttachments(prompt.Content, prompt.Attachments))
	if message.Event != "user" || message.Message.Content != want {
		t.Fatalf("message = %+v, want a user event carrying %q", message, want)
	}
	if model, _ := flagValue(run.argv, "--model"); model != "gemini-3.8-flash-low" {
		t.Fatalf("argv %q, want the configured model", run.argv)
	}
	for _, flag := range []string{"--dangerously-skip-permissions", "--input-format", "--output-format"} {
		if !slices.Contains(run.argv, flag) {
			t.Fatalf("argv %q lacks %s", run.argv, flag)
		}
	}
}

func TestSecondTurnContinuesTheConversationOfTheFirst(t *testing.T) {
	backend := NewBackend(t.Context(), Config{Cmd: agyStub(t)})
	t.Setenv("AGY_STUB_TURN", "1")
	if _, err := backend.Respond(t.Context(), contracts.Prompt{Content: "one"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, resumed := flagValue(lastRun(t).argv, "--conversation"); resumed {
		t.Fatal("first turn of a new session passed --conversation")
	}
	t.Setenv("AGY_STUB_TURN", "2")
	if _, err := backend.Respond(t.Context(), contracts.Prompt{Content: "two"}, nil); err != nil {
		t.Fatal(err)
	}
	if conversation, _ := flagValue(lastRun(t).argv, "--conversation"); conversation != "conv-1" {
		t.Fatalf("second turn resumed %q, want conv-1", conversation)
	}
	aware, ok := backend.(contracts.ResumeAware)
	if !ok || aware.ResumeToken() != "conv-2" {
		t.Fatal("resume token does not follow the last conversation agy reported")
	}
}

func TestPersistedResumeIsPassedOnTheFirstTurn(t *testing.T) {
	backend := NewBackend(t.Context(), Config{Cmd: agyStub(t), ResumeID: "conv-9", Effort: "high"})
	t.Setenv("AGY_STUB_TURN", "9")
	if _, err := backend.Respond(t.Context(), contracts.Prompt{Content: "again"}, nil); err != nil {
		t.Fatal(err)
	}
	argv := lastRun(t).argv
	if conversation, _ := flagValue(argv, "--conversation"); conversation != "conv-9" {
		t.Fatalf("argv %q, want --conversation conv-9", argv)
	}
	if effort, _ := flagValue(argv, "--effort"); effort != "high" {
		t.Fatalf("argv %q, want --effort high", argv)
	}
}

func TestErrorResultIsATurnFailure(t *testing.T) {
	backend := NewBackend(t.Context(), Config{Cmd: agyStub(t)})
	t.Setenv("AGY_STUB_TURN", "1")
	t.Setenv("AGY_STUB_STATUS", "ERROR")
	t.Setenv("AGY_STUB_ERROR", "quota exhausted")

	_, err := backend.Respond(t.Context(), contracts.Prompt{Content: "hi"}, nil)
	failed, isFailed := errors.AsType[*TurnFailedError](err)
	if !isFailed || failed.Message != "quota exhausted" {
		t.Fatalf("err = %v, want the error of the result", err)
	}
}

func TestTurnReturnsSoonAfterTheResultWhenAgyLingers(t *testing.T) {
	t.Parallel()
	stub := writeStubScript(t, `cat > /dev/null
printf '{"event":"result","result":{"conversation_id":"conv-1","status":"ERROR","error":"503 no capacity"}}\n'
exec sleep 600`)
	backend := NewBackend(t.Context(), Config{Cmd: stub})
	started := time.Now()
	_, err := backend.Respond(t.Context(), contracts.Prompt{Content: "hi"}, nil)
	if elapsed := time.Since(started); elapsed > resultExitGrace+5*time.Second {
		t.Fatalf("Respond took %v, want it bounded by the exit grace after the result", elapsed)
	}
	failed, isFailed := errors.AsType[*TurnFailedError](err)
	if !isFailed || failed.Message != "503 no capacity" {
		t.Fatalf("err = %v, want the failure of the result and no error of our own kill", err)
	}
}

func TestLingeringSuccessfulTurnIsNotAFailure(t *testing.T) {
	t.Parallel()
	stub := writeStubScript(t, `cat > /dev/null
printf '{"event":"result","result":{"conversation_id":"conv-1","status":"SUCCESS","response":"ok"}}\n'
exec sleep 600`)
	backend := NewBackend(t.Context(), Config{Cmd: stub})
	text, err := backend.Respond(t.Context(), contracts.Prompt{Content: "hi"}, nil)
	if err != nil || text != "ok" {
		t.Fatalf("text = %q, err = %v, want the answer and no error", text, err)
	}
}
