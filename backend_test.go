package antigravity

import (
	"errors"
	"io/fs"
	"os/exec"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestEmptyPromptNeverStartsAgy(t *testing.T) {
	backend := NewBackend(t.Context(), Config{Cmd: filepath.Join(t.TempDir(), "absent")})
	_, err := backend.Respond(t.Context(), contracts.Prompt{Content: "  "}, nil)
	if !errors.Is(err, ErrEmptyPrompt) {
		t.Fatalf("err = %v, want ErrEmptyPrompt", err)
	}
}

func TestMissingBinaryFailsTheTurn(t *testing.T) {
	backend := NewBackend(t.Context(), Config{Cmd: filepath.Join(t.TempDir(), "absent")})
	_, err := backend.Respond(t.Context(), contracts.Prompt{Content: "hi"}, nil)
	if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("err = %v, want the missing binary", err)
	}
}
