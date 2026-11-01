// Package deploycheck holds static checks over the deployment descriptors that
// live in the repository root (render.yaml, docker-compose.yml, the Dockerfile).
//
// These files are not compiled, so nothing else catches a mistake in them: a
// Blueprint that sets FORGECRM_ENV=production without a usable base URL makes the
// API refuse to start on the platform, and that only shows after a real deploy.
// The checks here read the files and assert the invariants the code depends on,
// so the failure appears in the ordinary unit suite instead.
package deploycheck

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// repoRoot is the module root, three levels above this package
// (backend/internal/deploycheck -> backend -> repo root is two; the test file
// sits one deeper, so it walks up three).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// blueprintEnvVar is one environment variable entry in a Blueprint service.
type blueprintEnvVar struct {
	Key         string `yaml:"key"`
	Value       string `yaml:"value"`
	FromService *struct {
		Type      string `yaml:"type"`
		Name      string `yaml:"name"`
		EnvVarKey string `yaml:"envVarKey"`
	} `yaml:"fromService"`
	FromDatabase *struct {
		Name     string `yaml:"name"`
		Property string `yaml:"property"`
	} `yaml:"fromDatabase"`
}

// blueprintService is one service in render.yaml.
type blueprintService struct {
	Name          string            `yaml:"name"`
	Type          string            `yaml:"type"`
	Runtime       string            `yaml:"runtime"`
	DockerCommand string            `yaml:"dockerCommand"`
	EnvVars       []blueprintEnvVar `yaml:"envVars"`
}

// blueprint is the subset of render.yaml this package reads.
type blueprint struct {
	Services []blueprintService `yaml:"services"`
}

// envValue reports how a service supplies an environment variable: an explicit
// value, or a reference to another service's variable. ok is false when the
// service does not set the key at all.
func (s blueprintService) envValue(key string) (value, fromServiceKey string, ok bool) {
	for _, e := range s.EnvVars {
		if e.Key != key {
			continue
		}
		if e.FromService != nil {
			return "", e.FromService.EnvVarKey, true
		}
		return e.Value, "", true
	}
	return "", "", false
}

func loadBlueprint(t *testing.T) blueprint {
	t.Helper()
	path := filepath.Join(repoRoot(t), "render.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read render.yaml: %v", err)
	}
	var bp blueprint
	if err := yaml.Unmarshal(data, &bp); err != nil {
		t.Fatalf("parse render.yaml: %v", err)
	}
	return bp
}

// runsProduction reports whether a service is configured to run in production
// mode, which is the condition under which the code's production validation
// applies.
func (s blueprintService) runsProduction() bool {
	v, _, ok := s.envValue("FORGECRM_ENV")
	return ok && v == "production"
}

// TestBlueprintProductionServicesHaveABaseURL is the guard for the defect this
// change fixes: FORGECRM_ENV=production requires an https FORGECRM_BASE_URL, and
// the API refuses to start without one. Each production service must supply it —
// either explicitly or by copying RENDER_EXTERNAL_URL — or the deploy fails at
// startup with "FORGECRM_BASE_URL must use https in production".
func TestBlueprintProductionServicesHaveABaseURL(t *testing.T) {
	bp := loadBlueprint(t)
	if len(bp.Services) == 0 {
		t.Fatal("render.yaml declares no services")
	}

	checked := 0
	for _, svc := range bp.Services {
		if !svc.runsProduction() {
			continue
		}
		checked++

		value, refKey, ok := svc.envValue("FORGECRM_BASE_URL")
		if !ok {
			t.Errorf("render.yaml service %q sets production but supplies no FORGECRM_BASE_URL; "+
				"the API refuses to start in production without an https base URL", svc.Name)
			continue
		}
		switch {
		case value != "" && !strings.HasPrefix(value, "https://"):
			t.Errorf("render.yaml service %q sets FORGECRM_BASE_URL=%q, which is not https", svc.Name, value)
		case refKey != "" && refKey != "RENDER_EXTERNAL_URL":
			t.Errorf("render.yaml service %q references %s for FORGECRM_BASE_URL; "+
				"only RENDER_EXTERNAL_URL is guaranteed to be an https service URL", svc.Name, refKey)
		}
	}
	if checked == 0 {
		t.Fatal("no production service found in render.yaml; the check inspected nothing")
	}
}

// TestBlueprintWorkerRunsTheWorkerBinary guards the second defect: the image's
// entrypoint defaults to the API, so a worker service must select the worker
// binary either through its command or through FORGECRM_ROLE. Without one of
// them the "worker" service runs a second API and no background jobs are
// processed.
func TestBlueprintWorkerRunsTheWorkerBinary(t *testing.T) {
	bp := loadBlueprint(t)

	workers := 0
	for _, svc := range bp.Services {
		if svc.Type != "worker" {
			continue
		}
		workers++

		cmd := strings.TrimSpace(svc.DockerCommand)
		role, _, _ := svc.envValue("FORGECRM_ROLE")

		namesWorker := strings.Contains(cmd, "forgecrm-worker")
		roleWorker := role == "worker"
		if !namesWorker && !roleWorker {
			t.Errorf("render.yaml worker %q neither names the worker binary in dockerCommand nor sets "+
				"FORGECRM_ROLE=worker; it would start a second API instance instead", svc.Name)
		}
	}
	if workers == 0 {
		t.Fatal("render.yaml declares no worker service")
	}
}

// TestBlueprintWorkerInheritsSecrets checks the worker gets the same secret key
// and database as the API. A worker without the secret key cannot decrypt stored
// integration credentials, so its jobs fail at run time rather than at startup.
func TestBlueprintWorkerInheritsSecrets(t *testing.T) {
	bp := loadBlueprint(t)

	for _, svc := range bp.Services {
		if svc.Type != "worker" {
			continue
		}
		for _, key := range []string{"FORGECRM_DATABASE_URL", "FORGECRM_SECRET_KEY"} {
			if _, _, ok := svc.envValue(key); !ok {
				t.Errorf("render.yaml worker %q does not set %s", svc.Name, key)
			}
		}
	}
}

// TestEnvironmentDocumentationCoversConfigVariables keeps the environment
// reference from drifting behind the code.
//
// The defect this guards is a renamed or added setting that the code reads but
// the operator guide never mentions, so a deployment cannot know it exists. The
// check reads the variable names out of the config package (the file that reads
// almost every setting) and fails when one is absent from
// docs/deployment/environment-variables.md. It cannot see every variable — some
// are read elsewhere — but it catches the common case cheaply and in the ordinary
// unit suite.
func TestEnvironmentDocumentationCoversConfigVariables(t *testing.T) {
	root := repoRoot(t)

	src, err := os.ReadFile(filepath.Join(root, "backend", "internal", "config", "config.go"))
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "docs", "deployment", "environment-variables.md"))
	if err != nil {
		t.Fatalf("read environment-variables.md: %v", err)
	}

	// An environment variable name is an all-caps string with at least one
	// underscore, which excludes error-code literals such as "FORGECRM_ENV" used
	// in prose and the lowercase store names.
	nameRe := regexp.MustCompile(`"([A-Z][A-Z0-9]*_[A-Z0-9_]*)"`)
	docText := string(doc)

	var missing []string
	for _, m := range nameRe.FindAllStringSubmatch(string(src), -1) {
		name := m[1]
		if !strings.Contains(docText, "`"+name+"`") {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Strings(missing)
	t.Errorf("these environment variables are read by internal/config but not documented in "+
		"docs/deployment/environment-variables.md: %v\nAdd a row for each, or the operator guide "+
		"silently drifts behind the code.", missing)
}
