// Package pricing is the fallback price table the dashboard uses to put a
// number on a dispatch nobody billed for.
//
// Ported from `orchestrator/dashboard/pricing.py`. The contract is the part
// worth keeping in mind: a recorded `cost_usd` above zero is the truth and is
// never second-guessed — that is what the provider charged. The table is only
// consulted when the recorded cost is zero, which is the normal case for
// opencode and local codex, where there is no billing API to ask.
package pricing

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

//go:embed defaults/pricing.yaml
var defaultsFS embed.FS

// hardcodedFallback is the floor when nothing else resolves — deliberately
// low. Under-estimating a cost is a smaller lie than inventing a $99/1M price
// for a model the table has never heard of.
var hardcodedFallback = ModelPrice{Input: 1.00, Output: 4.00}

// ModelPrice is USD per million tokens. CacheRead and CacheWrite are the
// prompt-cache rates; zero means the row has none and cached tokens are
// priced as Input.
type ModelPrice struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

// Tokens is one dispatch's usage. In counts every input token, cache
// included; CacheRead and CacheWrite are the parts of In read from and
// written to the prompt cache (state.Spend's convention).
type Tokens struct {
	In, Out, CacheRead, CacheWrite int
}

// Cost is what a dispatch of this size costs. Negative token counts are read
// as zero rather than as a refund.
func (p ModelPrice) Cost(t Tokens) float64 {
	in, out := max(t.In, 0), max(t.Out, 0)
	read, write := max(t.CacheRead, 0), max(t.CacheWrite, 0)
	cost := float64(out) * p.Output
	if p.CacheRead > 0 && read <= in {
		cost += float64(read) * p.CacheRead
		in -= read
	}
	if p.CacheWrite > 0 && write <= in {
		cost += float64(write) * p.CacheWrite
		in -= write
	}
	return (cost + float64(in)*p.Input) / 1_000_000.0
}

// Table is the layered price list: a project's own `pricing.yaml` on top of
// the packaged defaults.
//
// A project key REPLACES the default row of the same name rather than merging
// into it — you cannot tweak `input` and inherit `output`. One row is one
// complete price, which is the only version of this anybody can hold in their
// head while reading a YAML file.
type Table struct {
	prices map[string]ModelPrice
	asOf   string
}

// AsOf is the packaged table's `as_of` date: when its prices were last
// checked against the providers' price lists.
func (t Table) AsOf() string { return t.asOf }

// Load reads the embedded defaults and layers a project override on top.
//
// Every failure is silent and local: a missing file, unreadable YAML, a row
// that is not a price. A dashboard that refused to render because somebody
// hand-edited `pricing.yaml` would be trading a wrong number for no page at
// all, and this table's whole purpose is being an estimate.
//
// projectRoot may be empty, meaning "defaults only".
func Load(projectRoot string) Table {
	merged := map[string]ModelPrice{}

	var asOf string
	body, err := defaultsFS.ReadFile("defaults/pricing.yaml")
	if err == nil {
		var head struct {
			AsOf string `yaml:"as_of"`
		}
		_ = yaml.Unmarshal(body, &head)
		asOf = head.AsOf
		for name, price := range parseModels(body) {
			merged[name] = price
		}
	}
	if projectRoot != "" {
		// #nosec G304 -- the project's own pricing.yaml, named by the operator
		if override, readErr := os.ReadFile(filepath.Join(projectRoot, "pricing.yaml")); readErr == nil {
			for name, price := range parseModels(override) {
				merged[name] = price
			}
		}
	}
	// Guarantee a `default` row so ForModel always finds something short of
	// the floor. Cheap insurance against a typo in the shipped YAML.
	if _, ok := merged["default"]; !ok {
		merged["default"] = hardcodedFallback
	}
	return Table{prices: merged, asOf: asOf}
}

// ForModel resolves a price: the exact name, then `default`, then the floor.
func (t Table) ForModel(model string) ModelPrice {
	if p, ok := t.prices[model]; ok {
		return p
	}
	if p, ok := t.prices["default"]; ok {
		return p
	}
	return hardcodedFallback
}

// Has reports whether the model has a row of its own, as opposed to falling
// through to `default`.
func (t Table) Has(model string) bool {
	_, ok := t.prices[model]
	return ok
}

// EstimateCost looks up and computes in one call.
func (t Table) EstimateCost(model string, tokens Tokens) float64 {
	return t.ForModel(model).Cost(tokens)
}

// ResolveCost is the rule the whole package exists for: a recorded cost above
// zero wins, and anything else — zero, missing, negative — is estimated from
// tokens.
//
// Note that a recorded cost of exactly zero is treated as "not recorded", not
// as "free". That is Python's rule and it is the right one here: the backends
// that genuinely cost nothing to call are the same ones that report no cost at
// all, so the two are indistinguishable in the row.
func (t Table) ResolveCost(recordedCost float64, model string, tokens Tokens) float64 {
	if recordedCost > 0 {
		return recordedCost
	}
	if model == "" {
		model = "default"
	}
	return t.EstimateCost(model, tokens)
}

// Models lists every key, sorted, for a UI that wants to show the table.
func (t Table) Models() []string {
	out := make([]string, 0, len(t.prices))
	for name := range t.prices {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// parseModels reads the `models:` map out of one pricing.yaml.
//
// Anything that is not that shape is nothing: a document that is not a
// mapping, a `models` that is not a mapping, a row that is not a mapping. The
// keys that survive are forced to floats, so `input: "3.00"` — which a YAML
// file written by hand will have — still reads as 3.
func parseModels(body []byte) map[string]ModelPrice {
	var doc struct {
		Models map[string]map[string]any `yaml:"models"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil
	}
	out := make(map[string]ModelPrice, len(doc.Models))
	for name, row := range doc.Models {
		if row == nil {
			continue
		}
		out[name] = ModelPrice{
			Input:      asFloat(row["input"]),
			Output:     asFloat(row["output"]),
			CacheRead:  asFloat(row["cache_read"]),
			CacheWrite: asFloat(row["cache_write"]),
		}
	}
	return out
}

func asFloat(raw any) float64 {
	switch v := raw.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			return f
		}
	}
	return 0
}
