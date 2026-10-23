package budget

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrInvalidLimits is an edit SetLimits refuses before touching the file.
var ErrInvalidLimits = errors.New("invalid budget limits")

// Limits is what `orch budget set` changes in one provider's window; a nil
// field keeps what the file says.
type Limits struct {
	TokenBudget  *int     `json:"token_budget,omitempty"`
	WindowHours  *float64 `json:"window_hours,omitempty"`
	ThresholdPct *float64 `json:"threshold_pct,omitempty"`
}

var providerName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// SetLimits edits one provider's window in one preset of the budgets.yaml at
// path — `orch budget set` and the dashboard's Quota page both go through
// it — and returns the window as the file now reads.
//
// The file is edited as a YAML node tree, so the operator's comments, order
// and other presets stay as written. The result is loaded back with
// LoadConfig before it replaces the file: an edit that would not load is
// never written. A provider the preset lacks is added only with every field.
func SetLimits(path, preset, provider string, l Limits) (ProviderBudget, error) {
	if err := l.validate(provider); err != nil {
		return ProviderBudget{}, err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- the budgets path comes from the operator's own config
	if err != nil {
		if os.IsNotExist(err) {
			return ProviderBudget{}, fmt.Errorf("no budgets.yaml at %s — `orch init` writes one", path)
		}
		return ProviderBudget{}, fmt.Errorf("read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return ProviderBudget{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return ProviderBudget{}, fmt.Errorf("%s: not a mapping with presets", path)
	}
	presets := child(doc.Content[0], "presets")
	if presets == nil || presets.Kind != yaml.MappingNode {
		return ProviderBudget{}, &PresetNotFoundError{Path: path, Preset: preset}
	}
	spec := child(presets, preset)
	if spec == nil || spec.Kind != yaml.MappingNode {
		names := make([]string, 0, len(presets.Content)/2)
		for i := 0; i+1 < len(presets.Content); i += 2 {
			names = append(names, presets.Content[i].Value)
		}
		return ProviderBudget{}, &PresetNotFoundError{Path: path, Preset: preset, Available: names}
	}
	entry := child(spec, provider)
	if entry == nil {
		if l.TokenBudget == nil || l.WindowHours == nil || l.ThresholdPct == nil {
			return ProviderBudget{}, fmt.Errorf("%w: %s is not in preset %s; a new provider needs token_budget, window_hours and threshold_pct",
				ErrInvalidLimits, provider, preset)
		}
		entry = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		spec.Content = append(spec.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: provider}, entry)
	}
	if l.WindowHours != nil {
		setScalar(entry, "window_hours", strconv.FormatFloat(*l.WindowHours, 'f', -1, 64))
	}
	if l.TokenBudget != nil {
		setScalar(entry, "token_budget", strconv.Itoa(*l.TokenBudget))
	}
	if l.ThresholdPct != nil {
		setScalar(entry, "threshold_pct", strconv.FormatFloat(*l.ThresholdPct, 'f', -1, 64))
	}

	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return ProviderBudget{}, fmt.Errorf("encode %s: %w", path, err)
	}
	_ = enc.Close()

	// Load the result from a sibling file, then swap it in: the file on disk is
	// either the old one or a valid new one, never a half-written edit.
	info, err := os.Stat(path)
	if err != nil {
		return ProviderBudget{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".budgets-*.yaml")
	if err != nil {
		return ProviderBudget{}, fmt.Errorf("write next to %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(out.Bytes()); err != nil {
		_ = tmp.Close()
		return ProviderBudget{}, err
	}
	if err := tmp.Close(); err != nil {
		return ProviderBudget{}, err
	}
	cfg, err := LoadConfig(tmp.Name(), preset)
	if err != nil {
		return ProviderBudget{}, fmt.Errorf("%w: the edited file would not load: %v", ErrInvalidLimits, err)
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return ProviderBudget{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return ProviderBudget{}, fmt.Errorf("replace %s: %w", path, err)
	}
	return cfg.Providers[provider], nil
}

func (l Limits) validate(provider string) error {
	var problems []string
	if !providerName.MatchString(provider) {
		problems = append(problems, fmt.Sprintf("%q is not a provider name", provider))
	}
	if l.TokenBudget == nil && l.WindowHours == nil && l.ThresholdPct == nil {
		problems = append(problems, "nothing to change: give a token budget, a window or a threshold")
	}
	if l.TokenBudget != nil && *l.TokenBudget <= 0 {
		problems = append(problems, "token_budget must be more than 0")
	}
	if l.WindowHours != nil && *l.WindowHours <= 0 {
		problems = append(problems, "window_hours must be more than 0")
	}
	if l.ThresholdPct != nil && (*l.ThresholdPct <= 0 || *l.ThresholdPct > 100) {
		problems = append(problems, "threshold_pct must be more than 0 and at most 100")
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidLimits, strings.Join(problems, "; "))
	}
	return nil
}

// child is a mapping node's value for key, nil when absent.
func child(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setScalar sets key's plain scalar in mapping m, appending the key when
// absent; a comment on the value stays.
func setScalar(m *yaml.Node, key, value string) {
	if n := child(m, key); n != nil {
		n.Kind, n.Tag, n.Value, n.Style = yaml.ScalarNode, "", value, 0
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value})
}
