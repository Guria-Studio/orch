package scaffold

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hectorcanaimero/orch/internal/doctor"
)

// AllowOrchTools adds the rules of doctor.OrchToolRules that are missing to
// the project's .claude/settings.json, creating it when there is none, and
// returns what it added. It only adds: a rule the operator wrote, an allow or
// a deny, is never removed or reordered, and the file's other keys keep their
// order. A file that is not a JSON object is an error, not a rewrite.
//
// settings.json rather than settings.local.json: it is the one a worktree
// checks out, and the engine passes its permissions to every claude dispatch
// (#361).
func AllowOrchTools(root string) ([]string, error) {
	missing := doctor.MissingOrchRules(root)
	if len(missing) == 0 {
		return nil, nil
	}
	path := filepath.Join(root, ".claude", "settings.json")
	raw, err := os.ReadFile(path) // #nosec G304 -- the project's own settings file
	switch {
	case errors.Is(err, os.ErrNotExist):
		raw = []byte("{}")
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	top, err := decodeOrdered(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not a JSON object (%w); add %v to permissions.allow by hand", path, err, missing)
	}
	perms := &orderedObject{}
	if p, ok := top.get("permissions"); ok {
		if perms, err = decodeOrdered(p); err != nil {
			return nil, fmt.Errorf("%s: permissions is not an object (%w)", path, err)
		}
	}
	var allow []json.RawMessage
	if a, ok := perms.get("allow"); ok {
		if err := json.Unmarshal(a, &allow); err != nil {
			return nil, fmt.Errorf("%s: permissions.allow is not a list (%w)", path, err)
		}
	}
	for _, rule := range missing {
		q, _ := json.Marshal(rule)
		allow = append(allow, q)
	}
	a, _ := json.Marshal(allow)
	perms.set("allow", a)
	top.set("permissions", perms.encode())

	var out bytes.Buffer
	if err := json.Indent(&out, top.encode(), "", "  "); err != nil {
		return nil, fmt.Errorf("format %s: %w", path, err)
	}
	out.WriteByte('\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	return missing, nil
}

// orderedObject is a JSON object that remembers its key order, so a file
// orch adds to reads as the operator left it.
type orderedObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func decodeOrdered(raw []byte) (*orderedObject, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	o := &orderedObject{vals: map[string]json.RawMessage{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if _, dup := o.vals[key]; !dup {
			o.keys = append(o.keys, key)
		}
		o.vals[key] = v
	}
	return o, nil
}

func (o *orderedObject) get(key string) (json.RawMessage, bool) {
	v, ok := o.vals[key]
	return v, ok
}

func (o *orderedObject) set(key string, v json.RawMessage) {
	if o.vals == nil {
		o.vals = map[string]json.RawMessage{}
	}
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

func (o *orderedObject) encode() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		q, _ := json.Marshal(k)
		b.Write(q)
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}
