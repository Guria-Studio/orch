package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// CheckComposeIsolation warns about docker compose files that collide when
// two orch tasks bring them up at the same time from sibling worktrees (#343):
// a fixed top-level `name:`, which puts every task's containers in one
// project, and a fixed published host port, which only one of them can bind.
//
// It reads the compose files at the project root and one directory down —
// `docker-compose*.yml` and `compose*.yml`, either extension — skipping
// hidden directories and the usual dependency trees. orch never rewrites a
// compose file; the remediation is the convention that keeps tasks apart.
//
// isolate is dispatch.isolate_compose: with it on, orch's COMPOSE_PROJECT_NAME
// already beats a fixed `name:` for its own tasks, so the name is still
// reported (a `-p` or a run outside orch shares it) but the fix differs.
func CheckComposeIsolation(root string, isolate bool) Check {
	const name = "compose.isolation"
	files, err := composeFiles(root)
	if err != nil {
		return Check{Name: name, Status: StatusWarn, Detail: "could not look for compose files: " + err.Error()}
	}
	if len(files) == 0 {
		return Check{Name: name, Status: StatusOK, Detail: "no compose file in the project root or one level down"}
	}

	var findings []string
	fixedName, fixedPort := false, false
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- a compose file found under the project root
		if err != nil {
			findings = append(findings, fmt.Sprintf("%s: could not read it: %v", rel, err))
			continue
		}
		var doc composeDoc
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			findings = append(findings, fmt.Sprintf("%s: could not parse it: %v", rel, err))
			continue
		}
		var issues []string
		if doc.Name != "" {
			fixedName = true
			issues = append(issues, fmt.Sprintf("name: %q", doc.Name))
		}
		services := make([]string, 0, len(doc.Services))
		for svc := range doc.Services {
			services = append(services, svc)
		}
		sort.Strings(services)
		for _, svc := range services {
			for _, port := range doc.Services[svc].Ports {
				if host := fixedHostPort(port); host != "" {
					fixedPort = true
					issues = append(issues, fmt.Sprintf("%s publishes host port %s", svc, host))
				}
			}
		}
		if len(issues) > 0 {
			findings = append(findings, rel+": "+strings.Join(issues, ", "))
		}
	}
	if len(findings) == 0 {
		return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf(
			"%d compose file(s), none with a fixed project name or host port", len(files))}
	}

	var fix []string
	if fixedName {
		if isolate {
			fix = append(fix, "A fixed top-level name: is overridden by the COMPOSE_PROJECT_NAME orch gives each task "+
				"(dispatch.isolate_compose), but a `-p` or a compose run outside orch still shares it; drop it if nothing needs it.")
		} else {
			fix = append(fix, "dispatch.isolate_compose is off, so parallel tasks share one compose project and recreate "+
				"or remove each other's containers: set `dispatch: isolate_compose: true`, or drop the fixed name:.")
		}
	}
	if fixedPort {
		fix = append(fix, "Only one running task can bind a fixed host port. Publish it through a variable with a default, "+
			`ports: "${API_PORT:-3001}:3001", and derive the variable from ORCH_TASK_SLOT (1, 2, … per running task), `+
			"e.g. dispatch.worktree_setup writing `API_PORT=$((3001 + ORCH_TASK_SLOT))` to a gitignored .env.")
	}
	if len(fix) == 0 {
		fix = append(fix, "Fix or remove the file so it can be checked.")
	}
	return Check{
		Name: name, Status: StatusWarn,
		Detail:      "compose settings that collide when tasks run in parallel worktrees — " + strings.Join(findings, "; "),
		Remediation: strings.Join(fix, " ") + " See docs/CONFIG.md (dispatch.isolate_compose).",
	}
}

// composeDoc is the slice of a compose file this check reads.
type composeDoc struct {
	Name     string `yaml:"name"`
	Services map[string]struct {
		Ports []yaml.Node `yaml:"ports"`
	} `yaml:"services"`
}

// skipDirs are directories one level down that hold someone else's files.
var skipDirs = map[string]bool{"node_modules": true, "vendor": true}

// composeFiles lists the compose files at root and one directory down, as
// paths relative to root, sorted.
func composeFiles(root string) ([]string, error) {
	var out []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			if isComposeFile(e.Name()) {
				out = append(out, e.Name())
			}
			continue
		}
		if strings.HasPrefix(e.Name(), ".") || skipDirs[e.Name()] {
			continue
		}
		sub, err := os.ReadDir(filepath.Join(root, e.Name()))
		if err != nil {
			// A directory this user cannot read holds nothing orch's
			// agents can bring up either.
			continue
		}
		for _, s := range sub {
			if !s.IsDir() && isComposeFile(s.Name()) {
				out = append(out, filepath.Join(e.Name(), s.Name()))
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// isComposeFile matches the names docker compose looks for and the variants
// projects keep beside them: docker-compose*.yml and compose*.yml, either
// extension.
func isComposeFile(name string) bool {
	if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
		return false
	}
	return strings.HasPrefix(name, "docker-compose") || strings.HasPrefix(name, "compose")
}

// interpolation is a compose variable reference, `${VAR}` or `${VAR:-default}`.
var interpolation = regexp.MustCompile(`\$\{[^}]*\}`)

// fixedHostPort returns the host port a ports entry publishes when it is a
// literal, or "" when there is none (container port only, an ephemeral host
// port) or it comes from a variable.
//
// Short syntax is `[[IP:]HOST:]CONTAINER[/PROTOCOL]`; long syntax is a mapping
// whose `published` key is the host port. A bare number — `- 3001` — is the
// container port only.
func fixedHostPort(n yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		spec := interpolation.ReplaceAllString(n.Value, "$$VAR")
		spec, _, _ = strings.Cut(spec, "/")
		parts := strings.Split(spec, ":")
		if len(parts) < 2 {
			return ""
		}
		return literalPort(parts[len(parts)-2])
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "published" {
				return literalPort(interpolation.ReplaceAllString(n.Content[i+1].Value, "$$VAR"))
			}
		}
	}
	return ""
}

// literalPort is host when it names a port by value, else "".
func literalPort(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || strings.Contains(host, "$") {
		return ""
	}
	return host
}
