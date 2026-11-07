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
// entrypoint defaults to the API, so a worker service must select a non-API
// binary either through its command or through FORGECRM_ROLE. Without one of
// them the "worker" service runs a second API and no background jobs are
// processed. More than one non-API binary exists now (the worker and the
// standalone Telegram bot), so the check accepts any of them by name and falls
// back to a non-empty role.
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

		namesNonAPI := strings.Contains(cmd, "forgecrm-worker") || strings.Contains(cmd, "forgecrm-telegram")
		roleSet := role != ""
		if !namesNonAPI && !roleSet {
			t.Errorf("render.yaml worker %q neither names a non-API binary in dockerCommand nor sets "+
				"FORGECRM_ROLE; it would start a second API instance instead", svc.Name)
		}
	}
	if workers == 0 {
		t.Fatal("render.yaml declares no worker service")
	}
}

// TestBlueprintTelegramRunsTheTelegramBinary guards the same defect for the
// standalone bot: it must name its own binary and set a role, so it never comes
// up as a second API on a platform that drops one of the two signals.
func TestBlueprintTelegramRunsTheTelegramBinary(t *testing.T) {
	bp := loadBlueprint(t)

	var telegram *blueprintService
	for i := range bp.Services {
		if bp.Services[i].Name == "forgecrm-telegram" {
			telegram = &bp.Services[i]
			break
		}
	}
	if telegram == nil {
		t.Fatal("render.yaml declares no forgecrm-telegram service")
	}

	if !strings.Contains(telegram.DockerCommand, "forgecrm-telegram") {
		t.Errorf("render.yaml service %q does not name the telegram binary in dockerCommand", telegram.Name)
	}
	if role, _, _ := telegram.envValue("FORGECRM_ROLE"); role != "telegram" {
		t.Errorf("render.yaml service %q sets FORGECRM_ROLE=%q, want telegram", telegram.Name, role)
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

// composeService is the subset of one docker-compose.yml service these checks
// read.
type composeService struct {
	Name        string            `yaml:"name"`
	Image       string            `yaml:"image"`
	Entrypoint  []string          `yaml:"entrypoint"`
	Command     []string          `yaml:"command"`
	Environment map[string]string `yaml:"environment"`
}

// composeFile is the subset of docker-compose.yml this package reads.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

// TestDockerfilesOmitTheSyntaxDirective guards a deploy-only failure.
//
// `# syntax=docker/dockerfile:1` asks the builder to pull the BuildKit frontend
// image before it reads the rest of the file. A builder that cannot reach that
// image fails on the very first line, and the host then falls back to a
// Dockerfile it writes itself. Neither Dockerfile here uses a BuildKit-only
// feature (no --mount, --platform or heredoc), so the directive buys nothing and
// risks the whole build.
func TestDockerfilesOmitTheSyntaxDirective(t *testing.T) {
	root := repoRoot(t)

	for _, rel := range []string{"Dockerfile", filepath.Join("backend", "Dockerfile")} {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "# syntax=") {
				t.Errorf("%s:%d sets %q; the directive makes the builder fetch the BuildKit frontend "+
					"before it reads the file, which fails on hosts that cannot reach it and makes them "+
					"fall back to a Dockerfile of their own. No BuildKit-only feature is used here, so "+
					"remove the line.", rel, i+1, trimmed)
			}
			// The directive is only meaningful at the top of the file; a later
			// one is dead text, but still worth flagging as a stray copy.
			if i > 5 && strings.HasPrefix(trimmed, "# syntax=") {
				t.Errorf("%s:%d has a stray %q far from the top of the file", rel, i+1, trimmed)
			}
		}
	}
}

// TestRootBuildStampsTheRevisionFromTheCheckout guards the fix for the stale
// commit defect: a platform that builds the repository-root Dockerfile passes no
// docker build arguments, so the image used to report whatever a human had last
// typed into FORGECRM_COMMIT. The fix derives the revision from the checkout at
// build time. That only works while the build context actually contains .git, so
// this test asserts two invariants that together make the stamp trustworthy:
//
//   - the root Dockerfile reads the revision from a git checkout (a buildmeta
//     stage that runs `git rev-parse HEAD`), rather than trusting the
//     environment alone;
//   - .dockerignore does not exclude .git, because excluding it would silently
//     return the build to the "unknown" fallback and the stale-variable defect
//     with it.
func TestRootBuildStampsTheRevisionFromTheCheckout(t *testing.T) {
	root := repoRoot(t)

	dockerfile, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	src := string(dockerfile)
	if !strings.Contains(src, "git -C /meta rev-parse HEAD") {
		t.Error("the root Dockerfile does not resolve the revision from the git checkout; " +
			"without it the commit reported by /ready comes from a hand-maintained environment " +
			"variable, which is exactly the defect this change removes")
	}
	if !strings.Contains(src, "-X main.commit=") {
		t.Error("the root Dockerfile does not pass -X main.commit to the linker")
	}

	ignore, err := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if err != nil {
		t.Fatalf("read .dockerignore: %v", err)
	}
	for _, line := range strings.Split(string(ignore), "\n") {
		// A bare `.git` entry excludes the directory from the build context. The
		// buildmeta stage needs it, so its presence here is the regression.
		if strings.TrimSpace(line) == ".git" {
			t.Error(".dockerignore excludes .git, so the buildmeta stage cannot read the " +
				"checked-out revision and /ready falls back to the stale environment variable again")
		}
	}
}

// TestComposeWorkerSelectsTheWorkerBinary guards the second defect on the
// compose path: the image ENTRYPOINT defaults to the API, so the worker service
// must select the other binary. It must do so by more than one signal, because
// platforms differ in what they carry over: Compose honours `entrypoint` and
// `environment`, while blitz.cloud ignores `entrypoint` and maps `command` to
// its per-part start command. A service that relies on FORGECRM_ROLE alone runs
// a second API wherever the platform drops compose `environment`.
func TestComposeWorkerSelectsTheWorkerBinary(t *testing.T) {
	root := repoRoot(t)

	data, err := os.ReadFile(filepath.Join(root, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		t.Fatalf("parse docker-compose.yml: %v", err)
	}

	worker, ok := cf.Services["worker"]
	if !ok {
		t.Fatal("docker-compose.yml declares no worker service")
	}

	role := worker.Environment["FORGECRM_ROLE"]
	roleWorker := role == "worker"
	commandWorker := len(worker.Command) > 0 && (worker.Command[0] == "worker" ||
		strings.HasSuffix(worker.Command[0], "forgecrm-worker"))
	entrypointWorker := len(worker.Entrypoint) > 0 &&
		strings.Contains(strings.Join(worker.Entrypoint, " "), "forgecrm-entrypoint")

	signals := 0
	for _, set := range []bool{roleWorker, commandWorker, entrypointWorker} {
		if set {
			signals++
		}
	}
	if signals < 2 {
		t.Errorf("docker-compose.yml worker selects the worker binary by %d signal(s) "+
			"(FORGECRM_ROLE=worker: %v, command names the worker: %v, entrypoint is the dispatcher: %v); "+
			"at least two are needed, because a platform that ignores `entrypoint` and `environment` "+
			"would start a second API on this part and process no background job",
			signals, roleWorker, commandWorker, entrypointWorker)
	}
}

// TestComposeTelegramSelectsTheTelegramBinary is the same guard for the
// standalone bot: the image ENTRYPOINT defaults to the API, so the telegram
// service must select its own binary by at least two signals, or a platform that
// drops one of them runs a second API instead of the bot.
func TestComposeTelegramSelectsTheTelegramBinary(t *testing.T) {
	root := repoRoot(t)

	data, err := os.ReadFile(filepath.Join(root, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		t.Fatalf("parse docker-compose.yml: %v", err)
	}

	bot, ok := cf.Services["telegram"]
	if !ok {
		t.Fatal("docker-compose.yml declares no telegram service")
	}

	role := bot.Environment["FORGECRM_ROLE"]
	roleTelegram := role == "telegram"
	commandTelegram := len(bot.Command) > 0 && (bot.Command[0] == "telegram" ||
		strings.HasSuffix(bot.Command[0], "forgecrm-telegram"))
	entrypointTelegram := len(bot.Entrypoint) > 0 &&
		strings.Contains(strings.Join(bot.Entrypoint, " "), "forgecrm-entrypoint")

	signals := 0
	for _, set := range []bool{roleTelegram, commandTelegram, entrypointTelegram} {
		if set {
			signals++
		}
	}
	if signals < 2 {
		t.Errorf("docker-compose.yml telegram selects the bot binary by %d signal(s) "+
			"(FORGECRM_ROLE=telegram: %v, command names the bot: %v, entrypoint is the dispatcher: %v); "+
			"at least two are needed, because a platform that ignores `entrypoint` and `environment` "+
			"would start a second API on this part",
			signals, roleTelegram, commandTelegram, entrypointTelegram)
	}
}
