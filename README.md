# herrscher-antigravity-backend

**The Antigravity CLI (`agy`) as a Herrscher backend.** It answers each turn by
running the locally installed `agy` binary headless, with the prompt on stdin
(`--input-format stream-json`) and the turn read back as a stream of events
(`--output-format stream-json`).

It is not an API client. It holds no key and reaches no Google endpoint itself.
Authentication belongs to your `agy` install: run `agy` once in a terminal and
sign in with your Google account.

Category: backend, the model edge. Ports: `contracts.Backend` and
`contracts.ResumeAware`. Status: live.

## Install

```bash
curl -fsSL https://antigravity.google/cli/install.sh | bash
agy
herrscher plugin add github.com/Herrscherd/herrscher-antigravity-backend
```

Building the module needs the Go toolchain go1.27.4: `go.mod` pins it with
`toolchain go1.27.4`, and a `go` command older than that fetches it on first
use.

## Configuration

| Setting | Default | What it is |
|---|---|---|
| `ANTIGRAVITY_CMD` | `agy` | the binary to run, split on spaces |
| `ANTIGRAVITY_MODEL` | | the model a session gets when it names none, an id of `agy models` |
| `ANTIGRAVITY_EFFORT` | | `low`, `medium`, `high`, `xhigh` or `max`; any other value fails the factory at start |
| `ANTIGRAVITY_DIR` | | the working directory the CLI runs in |

The `resume` setting is declared in the manifest with no environment variable.
The host feeds it at construction from the session it persisted, and an operator
does not set it.

## A turn

Each turn starts one `agy` process:

```
agy --input-format stream-json --output-format stream-json \
    --dangerously-skip-permissions [--model M] [--effort E] [--conversation ID]
```

The prompt is written on stdin as one line, `{"event":"user","message":{"content":"..."}}`,
and stdin is closed, so `agy` runs that single turn and exits. The prompt never
appears in argv. It carries the recalled memory fenced into a
`<memory data-only="true">` block, and the paths of any downloaded attachment as
`[Attached file: path]` markers. Inside the recalled memory, every opening or
closing `memory` tag is replaced by `[memory]` before fencing, attributes,
blank characters (non-breaking and zero width spaces included), letter case and
an unterminated tag at the end included, so recalled text cannot close the fence.
A prompt with no text and no attachment fails
with `ErrEmptyPrompt` before any process starts.

Every tool call is approved (`--dangerously-skip-permissions`): `agy` has no
permission hook a headless turn could answer, and a permission prompt would
block the turn until its context ends. The manifest says so with
`Gate: GrainNone`. Run the backend in the isolated worktree of a session, never
in a directory it must not change.

The stream maps onto `contracts.BackendEvent` as follows:

| `agy` event | Herrscher event |
|---|---|
| `init` | the conversation id of the turn |
| `step_update` of type `agent_response` | its `text_delta` pieces are joined into one `text` block per step, emitted when the step ends; its `usage` becomes a `usage` event |
| `step_update` of type `tool`, state `ACTIVE` | a `tool` event that opens the call `step-N`, with the command, path, query or URL of its parameters as `Detail` |
| `step_update` of type `tool`, state `DONE` | a `tool_done` in the `succeeded` state, or `failed` with `exit code N` for a `run_command` whose output says it exited with a code other than 0 |
| `step_update` of type `tool`, state `ERROR` or `CANCELED` | a `tool_done` in the `failed` state, with the first line of the error message (200 runes at most) |
| `result` | the `result` event, then the end of the turn |

A tool step seen for the first time in a finished state opens and closes its
call at once. A repeated finish closes nothing. A turn follows at most 256 open
and 4096 distinct calls; the first start past a bound logs one `slog.Warn`.

The `usage` of the final `result` of `agy` adds up the whole conversation, so it
is ignored. The `result` event carries the input, output and cache read tokens
summed over the steps of this turn. `agy` gives
no price: the catalogue leaves prices unknown and the cost is shown in tokens.

A result whose status is not `SUCCESS` ends the turn with a `TurnFailedError`
holding its error text, such as an unknown model or a sign in that is missing.

Once the `result` is read, `agy` gets 10 seconds to exit. A process still alive
after that (one was seen lingering a minute after a `503 No capacity` result) is
killed with its group, and the kill is not reported as an error: the turn ends
with the outcome of its `result`.

## Resume

`agy` exits after every headless turn, so continuity comes from the conversation
id of the stream. It is stored, passed as `--conversation` on the next turn and
exposed by `ResumeToken()`. When `agy` cannot find the conversation it was asked
to resume, it opens a new one without failing: the backend then emits a
`notice` with `CodeResumeLost` and follows the new conversation.

## Process

stderr is discarded unless `Verbose` is set and never folded into an error. A
stream line longer than 16 MiB ends the turn with a `LineTooLongError`, and a
stream that stops before its `result` ends it with a `StreamEndedError`.

Each turn child starts in its own process group on Unix. The group is killed as
soon as the direct child exits or the turn stops early, so a background process
left by a tool dies with the turn. A descendant that escaped the group and still
holds stdout gets one second before the read is cut. Outside Unix the child and
its descendants are killed as a tree with `taskkill /T /F` only when the turn is
cut early (a cancelled context, a read error or the grace after the result).
After a turn that ends normally nothing is killed outside Unix, so a background
process started by a tool keeps running.

## Models and health

`Plugin.Discover` runs `agy models` and reads its `id<TAB>label` lines. A
signed out install lists nothing, without error. The static catalogue holds the
models `agy` 1.5.18 listed.

`Plugin.Probe` runs `agy models`, which needs a sign in:

| What it finds | State | Detail | Remedy |
|---|---|---|---|
| a listing | `ok` | `health.connected` | |
| a sign in that is missing (`sign in`, `authentication required` or `authentication failed` in the output) | `off` | `health.not_logged_in` | run `agy` |
| an empty listing | `degraded` | `health.unreachable` (`no model listed`) | |
| the binary is not installed | `off` | `health.unreachable` (`command not found`) | |
| a named profile is asked for | `off` | `health.unreachable` (`profiles not supported`) | |

With `Deep`, a signed in install also runs one turn in plan mode (`--mode plan`,
on the `model` setting when set, in the `dir` setting). A refused sign in is
`down` with `health.credentials_refused`, a quota or rate limit is `degraded`
with `health.rate_limited`, a `dir` that does not exist is `down`, any other
failure is `down`, and an answered turn is `ok`.

## Quality

`scripts/check.sh` runs the chain of the house Go book: `go mod tidy -diff`,
`golangci-lint fmt` and `run`, the comment check, the tests with `-race`, the
build with `CGO_ENABLED=0` and `govulncheck`. Outside CI each step is capped at
6 GB of memory; under CI (`CI` set) it runs without the cap. The Windows job runs
the tests that need no shell script; the stub based tests live in `_unix_test.go`
files.
