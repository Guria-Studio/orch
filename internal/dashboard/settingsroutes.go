package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Guria-Studio/orch/internal/config"
)

func (s *Server) settingsRoutes() []route {
	return []route{
		{pattern: "GET /api/config/effective", name: "api_config_effective", handler: s.handleEffectiveConfig},
		{pattern: "PATCH /api/config", name: "api_config_edit", handler: s.handleConfigEdit},
	}
}

// settingEntry is one leaf of the effective config: its dotted key, as
// docs/CONFIG.md names it, and its value as YAML would print it.
type settingEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type settingSection struct {
	Name    string         `json:"name"`
	Entries []settingEntry `json:"entries"`
}

// effectiveConfigPayload is `orch config show` as data: the config merged
// with defaults, by section in the file's own order, the files it came from
// and the keys that were ignored. Error is a config.yaml that does not load.
type effectiveConfigPayload struct {
	Path     string           `json:"path"`
	Sections []settingSection `json:"sections"`
	Sources  []string         `json:"sources"`
	Warnings []string         `json:"warnings"`
	Error    string           `json:"error,omitempty"`
}

// handleEffectiveConfig is System › Settings' read. Operator-only: budgets,
// providers and hosts are the operator's.
func (s *Server) handleEffectiveConfig(w http.ResponseWriter, _ *http.Request) {
	out, err := s.effectiveConfig()
	if err != nil {
		s.failRead(w, "settings", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// configEditBody is PATCH /api/config's body: dotted keys to values, typed by
// the JSON (true, 3, "text", ["a", "b"]).
type configEditBody struct {
	Set map[string]any `json:"set"`
}

// handleConfigEdit saves the Settings page through config.Set — the function
// behind `orch config set` — all or nothing, and answers with the config as
// it now stands. The local operator only, as task edits are. A refused value
// is a 400 with config.Set's reason, which never repeats a credential.
func (s *Server) handleConfigEdit(w http.ResponseWriter, r *http.Request) {
	var body configEditBody
	if !s.operatorWrite(w, r, true, &body) {
		return
	}
	if len(body.Set) == 0 {
		writeJSON(w, http.StatusBadRequest, errorPayload{Detail: "nothing to set"})
		return
	}
	values := make(map[string]string, len(body.Set))
	for key, v := range body.Set {
		switch v := v.(type) {
		case string:
			values[key] = v
		case bool:
			values[key] = strconv.FormatBool(v)
		case float64:
			values[key] = strconv.FormatFloat(v, 'f', -1, 64)
		case []any:
			// A JSON array is a YAML flow sequence, so items keep their commas.
			b, _ := json.Marshal(v)
			values[key] = string(b)
		default:
			writeJSON(w, http.StatusBadRequest, errorPayload{Detail: key + ": want text, a number, true/false or a list"})
			return
		}
	}
	if err := config.Set(s.paths.ConfigYAML, s.paths.Root, values); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload{Detail: err.Error()})
		return
	}
	out, err := s.effectiveConfig()
	if err != nil {
		s.failRead(w, "settings", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// effectiveConfig is `orch config show` as the Settings page reads it.
func (s *Server) effectiveConfig() (effectiveConfigPayload, error) {
	out := effectiveConfigPayload{Path: s.paths.ConfigYAML, Sections: []settingSection{}, Sources: []string{}, Warnings: []string{}}
	res, err := config.Load(s.paths.ConfigYAML, s.paths.Root)
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	out.Sources = append(out.Sources, res.Sources...)
	out.Warnings = append(out.Warnings, res.Warnings...)

	var doc yaml.Node
	if err := doc.Encode(res.Config); err != nil {
		return out, err
	}
	// A mapping node's Content alternates key, value; order is the struct's.
	for i := 0; i+1 < len(doc.Content); i += 2 {
		name, value := doc.Content[i].Value, doc.Content[i+1]
		sec := settingSection{Name: name}
		flatten(name, value, &sec.Entries)
		out.Sections = append(out.Sections, sec)
	}
	return out, nil
}

// flatten turns a node into dotted leaves; a list or a scalar is one leaf,
// printed inline.
func flatten(key string, n *yaml.Node, into *[]settingEntry) {
	if n.Kind == yaml.MappingNode && len(n.Content) > 0 {
		for i := 0; i+1 < len(n.Content); i += 2 {
			flatten(key+"."+n.Content[i].Value, n.Content[i+1], into)
		}
		return
	}
	inline := *n
	inline.Style = yaml.FlowStyle
	b, err := yaml.Marshal(&inline)
	value := strings.TrimSpace(string(b))
	if err != nil {
		value = n.Value
	}
	if config.IsSecret(key) && value != "" && value != `""` {
		value = "(set)"
	}
	*into = append(*into, settingEntry{Key: key, Value: value})
}
