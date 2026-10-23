package dashboard

import (
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/hectorcanaimero/orch/internal/config"
)

func (s *Server) settingsRoutes() []route {
	return []route{
		{pattern: "GET /api/config/effective", name: "api_config_effective", handler: s.handleEffectiveConfig},
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

// handleEffectiveConfig is System › Settings: read-only, because the file is
// the interface — orch has no command that writes config.yaml, and the page
// would be the only one. Operator-only: budgets, providers and hosts are
// the operator's.
func (s *Server) handleEffectiveConfig(w http.ResponseWriter, _ *http.Request) {
	out := effectiveConfigPayload{Path: s.paths.ConfigYAML, Sections: []settingSection{}, Sources: []string{}, Warnings: []string{}}
	res, err := config.Load(s.paths.ConfigYAML, s.paths.Root)
	if err != nil {
		out.Error = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Sources = append(out.Sources, res.Sources...)
	out.Warnings = append(out.Warnings, res.Warnings...)

	var doc yaml.Node
	if err := doc.Encode(res.Config); err != nil {
		s.failRead(w, "settings", err)
		return
	}
	// A mapping node's Content alternates key, value; order is the struct's.
	for i := 0; i+1 < len(doc.Content); i += 2 {
		name, value := doc.Content[i].Value, doc.Content[i+1]
		sec := settingSection{Name: name}
		flatten(name, value, &sec.Entries)
		out.Sections = append(out.Sections, sec)
	}
	writeJSON(w, http.StatusOK, out)
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
	if secretKeys[key[strings.LastIndex(key, ".")+1:]] && value != "" && value != `""` {
		value = "(set)"
	}
	*into = append(*into, settingEntry{Key: key, Value: value})
}

// secretKeys are config leaves whose value is a credential: the stakeholder
// token and the notification webhooks (a webhook URL is enough to post).
// The page says whether they are set, never what they are.
var secretKeys = map[string]bool{"token": true, "slack_webhook": true, "discord_webhook": true}
