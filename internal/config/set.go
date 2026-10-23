package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Set writes each dotted key's value into the config file at path — the one
// function behind `orch config set` and the dashboard's Settings page. Values
// arrive as text and are typed by the Config field the key names, so
// `tunnel.enabled true` is a bool and `concurrency.global_max 3` an int.
//
// It is all or nothing: an unknown key, a value of the wrong type, or a
// result that config.Load refuses (a stakeholder profile with no token, say)
// leaves the file untouched. The file is edited as a YAML tree, so comments,
// key order and every key it does not touch survive.
func Set(path, projectRoot string, values map[string]string) error {
	return edit(path, projectRoot, func(root *yaml.Node) error {
		for key, raw := range values {
			t, err := keyType(key)
			if err != nil {
				return err
			}
			value, err := valueNode(key, t, raw)
			if err != nil {
				return err
			}
			setPath(root, strings.Split(key, "."), value)
		}
		return nil
	})
}

// Unset removes each key from the file, so it falls back to its default. A key
// the file does not set is not an error.
func Unset(path, projectRoot string, keys []string) error {
	return edit(path, projectRoot, func(root *yaml.Node) error {
		for _, key := range keys {
			if _, err := keyType(key); err != nil {
				return err
			}
			unsetPath(root, strings.Split(key, "."))
		}
		return nil
	})
}

// IsSecret reports a key whose value is a credential: the stakeholder token
// and the notification webhooks (a webhook URL is enough to post). Whatever
// prints config says whether they are set, never what they are.
func IsSecret(key string) bool {
	switch key[strings.LastIndex(key, ".")+1:] {
	case "token", "slack_webhook", "discord_webhook":
		return true
	}
	return false
}

// edit loads the file as a node tree, applies change, checks that the result
// loads, and only then replaces the file.
func edit(path, projectRoot string, change func(root *yaml.Node) error) error {
	var doc yaml.Node
	mode := os.FileMode(0o600)
	if b, err := os.ReadFile(path); err == nil { // #nosec G304 -- the project's own config path
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode().Perm()
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: the top level is not a mapping", path)
	}
	if err := change(root); err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config.yaml.*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if projectRoot == "" {
		projectRoot = dir
	}
	if _, err := Load(tmp.Name(), projectRoot); err != nil {
		return fmt.Errorf("the change was not saved: %w", err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// keyType is the Go type of the Config field a dotted key names, refusing a
// key orch does not read and a whole section (`tunnel` rather than
// `tunnel.enabled`).
func keyType(key string) (reflect.Type, error) {
	parent := key[:max(0, strings.LastIndex(key, "."))]
	if !knownKeys[key] && !knownKeys[parent+".*"] {
		return nil, fmt.Errorf("%s is not a config key orch reads (docs/CONFIG.md lists them)", key)
	}
	t := reflect.TypeOf(Config{})
	for _, part := range strings.Split(key, ".") {
		switch t.Kind() {
		case reflect.Struct:
			f, ok := fieldByYAML(t, part)
			if !ok {
				return nil, fmt.Errorf("%s is not a config key orch reads (docs/CONFIG.md lists them)", key)
			}
			t = f
		case reflect.Map:
			t = t.Elem()
		default:
			return nil, fmt.Errorf("%s is not a config key orch reads (docs/CONFIG.md lists them)", key)
		}
	}
	return t, nil
}

func fieldByYAML(t reflect.Type, name string) (reflect.Type, bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if tag == name {
			return f.Type, true
		}
	}
	return nil, false
}

// valueNode types raw as t: a bool, a whole number, a number, a list
// (`a, b` or `[a, b]`) or text.
func valueNode(key string, t reflect.Type, raw string) (*yaml.Node, error) {
	raw = strings.TrimSpace(raw)
	scalar := func(tag, v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: v} }
	switch t.Kind() {
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: want true or false, got %q", key, raw)
		}
		return scalar("!!bool", strconv.FormatBool(b)), nil
	case reflect.Int, reflect.Int64, reflect.Int32:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: want a whole number, got %q", key, raw)
		}
		return scalar("!!int", strconv.Itoa(n)), nil
	case reflect.Float64, reflect.Float32:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: want a number, got %q", key, raw)
		}
		return scalar("!!float", strconv.FormatFloat(f, 'g', -1, 64)), nil
	case reflect.String:
		return scalar("!!str", raw), nil
	case reflect.Slice:
		var items []string
		if strings.HasPrefix(raw, "[") {
			if err := yaml.Unmarshal([]byte(raw), &items); err != nil {
				return nil, fmt.Errorf("%s: want a list, got %q", key, raw)
			}
		} else {
			for _, item := range strings.Split(raw, ",") {
				if item = strings.TrimSpace(item); item != "" {
					items = append(items, item)
				}
			}
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range items {
			seq.Content = append(seq.Content, scalar("!!str", item))
		}
		return seq, nil
	}
	return nil, fmt.Errorf("%s is a section, not a value: set one of its keys", key)
}

// setPath puts value at the dotted path, creating the blocks on the way. An
// existing value's comments stay with its replacement.
func setPath(m *yaml.Node, parts []string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != parts[0] {
			continue
		}
		old := m.Content[i+1]
		if len(parts) == 1 {
			value.LineComment, value.HeadComment, value.FootComment = old.LineComment, old.HeadComment, old.FootComment
			m.Content[i+1] = value
			return
		}
		if old.Kind != yaml.MappingNode {
			m.Content[i+1] = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", LineComment: old.LineComment}
		}
		setPath(m.Content[i+1], parts[1:], value)
		return
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: parts[0]}
	if len(parts) == 1 {
		m.Content = append(m.Content, k, value)
		return
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	m.Content = append(m.Content, k, child)
	setPath(child, parts[1:], value)
}

func unsetPath(m *yaml.Node, parts []string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != parts[0] {
			continue
		}
		if len(parts) == 1 {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
		if m.Content[i+1].Kind == yaml.MappingNode {
			unsetPath(m.Content[i+1], parts[1:])
		}
		return
	}
}
