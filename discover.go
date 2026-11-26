package antigravity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	signInText       = "sign in"
	authRequiredText = "authentication required"
	authFailedText   = "authentication failed"
	boundedWaitDelay = 2 * time.Second
	boundedOutputMax = 256 << 10
	envCmd           = "ANTIGRAVITY_CMD"
)

var modelIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type cappedBuffer struct {
	data     bytes.Buffer
	exceeded bool
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	accepted := min(len(data), max(boundedOutputMax-b.data.Len(), 0))
	if accepted < len(data) {
		b.exceeded = true
	}
	if _, err := b.data.Write(data[:accepted]); err != nil {
		return 0, err
	}
	return len(data), nil
}

// OutputTooLargeError reports a command of agy whose output is longer than Limit
// bytes.
type OutputTooLargeError struct {
	Limit int
}

func (e *OutputTooLargeError) Error() string {
	return fmt.Sprintf("agy output is longer than %d bytes", e.Limit)
}

func discover(ctx context.Context, access contracts.DiscoveryAccess) ([]contracts.ModelSpec, error) {
	if !access.Policy.Allows(contracts.RouteNative) {
		return nil, nil
	}
	base := baseCommand(access.Getenv(envCmd))
	output, runErr := runBounded(ctx, append(slices.Clone(base), "models"), "", "")
	if runErr != nil && output.isSignedOut() {
		return nil, nil
	}
	if runErr != nil {
		return nil, fmt.Errorf("agy models: %w", errors.Join(runErr, ctx.Err()))
	}
	if output.stdout.exceeded {
		return nil, &OutputTooLargeError{Limit: boundedOutputMax}
	}
	return parseModelsOutput(output.stdout.data.String()), nil
}

type boundedOutput struct {
	stdout cappedBuffer
	stderr cappedBuffer
}

func runBounded(ctx context.Context, argv []string, dir, stdin string) (*boundedOutput, error) {
	output := &boundedOutput{}
	cmd := turnCommand(ctx, argv, dir, &output.stderr)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &output.stdout
	cmd.WaitDelay = boundedWaitDelay
	return output, cmd.Run()
}

func (o *boundedOutput) isSignedOut() bool {
	return o.mentions(signInText) || o.mentions(authRequiredText) || o.mentions(authFailedText)
}

func (o *boundedOutput) mentions(lowerText string) bool {
	return containsFold(o.stdout.data.String(), lowerText) || containsFold(o.stderr.data.String(), lowerText)
}

func containsFold(output, lowerText string) bool {
	return strings.Contains(strings.ToLower(output), lowerText)
}

func parseModelsOutput(output string) []contracts.ModelSpec {
	var specs []contracts.ModelSpec
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(output, "\n") {
		spec, ok := parseModelLine(line)
		if !ok || seen[spec.ID] {
			continue
		}
		seen[spec.ID] = true
		specs = append(specs, spec)
	}
	return specs
}

func parseModelLine(line string) (contracts.ModelSpec, bool) {
	modelID, label, isListed := strings.Cut(strings.TrimSpace(line), "\t")
	modelID = strings.TrimSpace(modelID)
	label = strings.TrimSpace(label)
	if !isListed || !modelIDPattern.MatchString(modelID) || label == "" || strings.ContainsFunc(label, isControl) {
		return contracts.ModelSpec{}, false
	}
	return contracts.ModelSpec{ID: modelID, Label: label, Arg: modelID, Route: contracts.RouteNative}, true
}

func isControl(char rune) bool {
	return char < ' ' || char == 0x7f
}
