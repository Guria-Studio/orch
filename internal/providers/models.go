package providers

import (
	"encoding/json"
	"strings"
)

// ModelLister is the optional half of a Provider whose CLI can say which
// models it runs now (#397): opencode, agy and codex. claude, gemini and muse
// have no such command, and their packaged routes stay as they are.
//
// Pure like the rest of the package: ModelsArgv is what to exec, ParseModels
// reads its stdout. Running it — with a timeout, on PATH — is the caller's.
type ModelLister interface {
	// ModelsArgv is the argv that prints the model list. It spends nothing
	// and opens no session.
	ModelsArgv() []string
	// ParseModels returns the model ids in out, in the CLI's order, as a
	// route's cli_model names them. Output it cannot read gives none.
	ParseModels(out []byte) []string
}

// ModelsArgv is `opencode models`.
func (OpencodeProvider) ModelsArgv() []string { return []string{"opencode", "models"} }

// ParseModels reads `opencode models`: one `provider/model` per line.
func (OpencodeProvider) ParseModels(out []byte) []string {
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) == 1 && strings.Contains(f[0], "/") {
			ids = append(ids, f[0])
		}
	}
	return ids
}

// ModelsArgv is `agy models`.
func (AgyProvider) ModelsArgv() []string { return []string{"agy", "models"} }

// ParseModels reads `agy models`: `id<TAB>display name` per line.
func (AgyProvider) ParseModels(out []byte) []string {
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		if id, _, ok := strings.Cut(line, "\t"); ok && id != "" && !strings.ContainsAny(id, " ") {
			ids = append(ids, id)
		}
	}
	return ids
}

// ModelsArgv is `codex debug models`, codex's model catalog as JSON. It reads
// no network; the catalog ships with the CLI.
func (CodexProvider) ModelsArgv() []string { return []string{"codex", "debug", "models"} }

// ParseModels reads the catalog's slugs that codex shows (visibility "list");
// the hidden ones are codex's own (its reviewer, reserved slugs).
func (CodexProvider) ParseModels(out []byte) []string {
	var cat struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if json.Unmarshal(out, &cat) != nil {
		return nil
	}
	var ids []string
	for _, m := range cat.Models {
		if m.Slug != "" && m.Visibility == "list" {
			ids = append(ids, m.Slug)
		}
	}
	return ids
}
