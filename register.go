package antigravity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// ErrInvalidSetting reports a setting whose value the factory cannot read.
var ErrInvalidSetting = errors.New("invalid antigravity setting")

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "antigravity",
			Category: contracts.CategoryBackend,
			Status:   contracts.StatusLive,
			Config: []contracts.Setting{
				{Key: "cmd", Env: envCmd, Help: "base command to run the Antigravity CLI", Default: defaultCmd},
				{Key: "model", Env: "ANTIGRAVITY_MODEL", Help: "model override"},
				{Key: "effort", Env: "ANTIGRAVITY_EFFORT", Help: "reasoning effort: low, medium, high, xhigh or max"},
				{Key: "dir", Env: "ANTIGRAVITY_DIR", Help: "working directory"},
				{Key: "resume", Help: "conversation id to resume, fed by the host at construction, never set by an operator"},
			},
			Models: Models(),
			Capabilities: contracts.Capabilities{
				Gate:    contracts.GrainNone,
				GateWhy: "agy runs headless with --dangerously-skip-permissions, a permission prompt would block the turn",
			},
		},
		Discover: discover,
		Probe:    probe,
		Backend: func(ctx context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			effort, err := effortSetting(cfg)
			if err != nil {
				return nil, err
			}
			return NewBackend(ctx, Config{
				Cmd:      cfg.Get("cmd"),
				Model:    cfg.Get("model"),
				Effort:   effort,
				Dir:      cfg.Get("dir"),
				ResumeID: cfg.Get("resume"),
			}), nil
		},
	})
}

func effortSetting(cfg contracts.PluginConfig) (string, error) {
	effort := strings.ToLower(strings.TrimSpace(cfg.Get("effort")))
	switch effort {
	case "", "low", "medium", "high", "xhigh", "max":
		return effort, nil
	}
	return "", fmt.Errorf("%w: effort=%q, want low, medium, high, xhigh or max", ErrInvalidSetting, effort)
}
