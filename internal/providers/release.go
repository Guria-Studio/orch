package providers

// Distribution is where a CLI's releases are published and how each way of
// installing it is updated (#448). orch only prints these; the operator runs
// them.
type Distribution struct {
	// NPM is the package on the npm registry: where the latest version is
	// read, and what `npm install -g` updates.
	NPM string
	// Brew is the Homebrew formula (or "--cask <name>").
	Brew string
	// Self is the CLI's own update command, for the copy its install script
	// puts under a path containing SelfPath; "" when it has none.
	Self     string
	SelfPath string
}

// Distributed is the optional half of a Provider whose CLI has a release feed
// orch can read. agy and muse have none: their latest version is unknown, and
// unknown is never a warning.
type Distributed interface {
	Distribution() Distribution
}

// Distribution is @anthropic-ai/claude-code; the native install updates itself.
func (ClaudeProvider) Distribution() Distribution {
	return Distribution{NPM: "@anthropic-ai/claude-code", Brew: "--cask claude-code", Self: "claude update", SelfPath: "/.local/share/claude/"}
}

// Distribution is @openai/codex.
func (CodexProvider) Distribution() Distribution {
	return Distribution{NPM: "@openai/codex", Brew: "codex"}
}

// Distribution is @google/gemini-cli.
func (GeminiProvider) Distribution() Distribution {
	return Distribution{NPM: "@google/gemini-cli", Brew: "gemini-cli"}
}

// Distribution is opencode-ai; its install script's copy has `opencode upgrade`.
func (OpencodeProvider) Distribution() Distribution {
	return Distribution{NPM: "opencode-ai", Brew: "opencode", Self: "opencode upgrade", SelfPath: "/.opencode/"}
}
