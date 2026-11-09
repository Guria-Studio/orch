package config

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every key the loader reads has its place in docs/CONFIG.md (#550). A key is
// found in the section of its top-level block — `### `+"`verify`"+` for
// verify.app.url — written as `+"`leaf:`"+` in a YAML example, as `+"`leaf`"+` in
// prose, or by its full dotted name; a top-level key anywhere under "## Keys".
// knownKeys is the list, and TestEveryKnownKeyHasAType ties it to the struct.
func TestConfigDocCoversEveryKey(t *testing.T) {
	b, err := os.ReadFile("../../docs/CONFIG.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	keys := doc[strings.Index(doc, "\n## Keys\n"):]
	keys = keys[:strings.Index(keys[1:], "\n## ")+1]

	var missing []string
	for key := range knownKeys {
		key = strings.TrimSuffix(key, ".*")
		parts := strings.Split(key, ".")
		section := keys
		if len(parts) > 1 {
			section = docSection(keys, parts[0])
		}
		leaf := regexp.QuoteMeta(parts[len(parts)-1])
		re := regexp.MustCompile("(?m)(^\\s*" + leaf + ":|`" + leaf + "`|" + regexp.QuoteMeta(key) + ")")
		if !re.MatchString(section) {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, k := range missing {
		t.Errorf("docs/CONFIG.md does not document %s", k)
	}
}

// docSection is the text from "### `name`" to the next "### " heading.
func docSection(doc, name string) string {
	i := strings.Index(doc, "\n### `"+name+"`")
	if i < 0 {
		return ""
	}
	s := doc[i+1:]
	if j := strings.Index(s[1:], "\n### "); j >= 0 {
		s = s[:j+1]
	}
	return s
}

// knownKeys is the list the doc test walks, so it must hold every leaf of the
// Config struct (the other direction is TestEveryKnownKeyHasAType). A field
// left out of it would be warned about as unknown and skipped by the doc test.
func TestKnownKeysCoverTheStruct(t *testing.T) {
	var walk func(prefix string, typ reflect.Type)
	walk = func(prefix string, typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			tag := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			key := strings.TrimPrefix(prefix+"."+tag, ".")
			if knownKeys[key] || knownKeys[key+".*"] {
				continue
			}
			if ft := typ.Field(i).Type; ft.Kind() == reflect.Struct {
				walk(key, ft)
				continue
			}
			t.Errorf("%s is a Config field missing from knownKeys", key)
		}
	}
	walk("", reflect.TypeOf(Config{}))
}
