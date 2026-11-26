package antigravity

import contracts "github.com/Herrscherd/herrscher-contracts"

// Models returns the agy catalogue as `agy models` lists it, native route only.
// Prices are left unknown: agy bills against the quota of the Google account.
func Models() []contracts.ModelSpec {
	return []contracts.ModelSpec{
		{ID: "gemini-4-hydrogen-high", Label: "Gemini 4 Hydrogen (High)", Arg: "gemini-4-hydrogen-high", Route: contracts.RouteNative},
		{ID: "gemini-4-hydrogen-xhigh", Label: "Gemini 4 Hydrogen (XHigh)", Arg: "gemini-4-hydrogen-xhigh", Route: contracts.RouteNative},
		{ID: "gemini-4-hydrogen-medium", Label: "Gemini 4 Hydrogen (Medium)", Arg: "gemini-4-hydrogen-medium", Route: contracts.RouteNative},
		{ID: "gemini-4-hydrogen-low", Label: "Gemini 4 Hydrogen (Low)", Arg: "gemini-4-hydrogen-low", Route: contracts.RouteNative, Hidden: true},
		{ID: "gemini-3.8-flash-high", Label: "Gemini 3.8 Flash (High)", Arg: "gemini-3.8-flash-high", Route: contracts.RouteNative},
		{ID: "gemini-3.8-flash-medium", Label: "Gemini 3.8 Flash (Medium)", Arg: "gemini-3.8-flash-medium", Route: contracts.RouteNative, Hidden: true},
		{ID: "gemini-3.8-flash-low", Label: "Gemini 3.8 Flash (Low)", Arg: "gemini-3.8-flash-low", Route: contracts.RouteNative, Hidden: true},
		{ID: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6 (Thinking)", Arg: "claude-sonnet-4-6", Route: contracts.RouteNative, Hidden: true},
		{ID: "claude-opus-4-6-thinking", Label: "Claude Opus 4.6 (Thinking)", Arg: "claude-opus-4-6-thinking", Route: contracts.RouteNative, Hidden: true},
		{ID: "gpt-oss-120b-medium", Label: "GPT-OSS 120B (Medium)", Arg: "gpt-oss-120b-medium", Route: contracts.RouteNative, Hidden: true},
	}
}
