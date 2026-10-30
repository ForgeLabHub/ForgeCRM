// Command openapi-gen writes the OpenAPI specification for the ForgeCRM API.
//
// It builds the real router — the same value the server serves — walks it for the
// paths and methods, and reads the permission annotations from the route
// registrations in internal/api. The output therefore cannot describe a route
// that does not exist, and cannot omit one that does.
//
// It also renders a self-contained HTML reference from the same document, so the
// browsable reference is a view of the specification rather than a second,
// hand-maintained description of the API.
//
// Usage:
//
//	go run ./cmd/openapi-gen              # write docs/api/openapi.json and docs/api/index.html
//	go run ./cmd/openapi-gen -check       # fail if either file is out of date
//	go run ./cmd/openapi-gen -out FILE    # write the specification elsewhere
//
// -check is what CI runs: it regenerates in memory and compares with the
// committed files, so a route added without regenerating the specification fails
// the build rather than shipping a stale document or a stale reference page.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/geff1778/forgecrm/internal/api"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/openapi"
	"github.com/geff1778/forgecrm/internal/ratelimit"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "openapi-gen:", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", "",
		"output path for the specification; defaults to docs/api/openapi.json at the repository root")
	siteOut := flag.String("site", "",
		"output path for the HTML reference; defaults to docs/api/index.html next to the specification")
	check := flag.Bool("check", false, "verify the files are current instead of writing them")
	version := flag.String("version", "", "version string for the document; defaults to 0.1.0")
	flag.Parse()

	// The default output is resolved against the repository root rather than the
	// working directory. Running the generator from the repository root instead of
	// backend/ is an easy mistake, and without this it would quietly write to
	// /docs instead of failing.
	outPath := *out
	sitePath := *siteOut
	if outPath == "" || sitePath == "" {
		root, err := repoRoot()
		if err != nil {
			return err
		}
		if outPath == "" {
			outPath = filepath.Join(root, "docs", "api", "openapi.json")
		}
		if sitePath == "" {
			sitePath = filepath.Join(root, "docs", "api", "index.html")
		}
	}

	doc, err := generate(*version)
	if err != nil {
		return err
	}
	site, err := openapi.Site(doc)
	if err != nil {
		return err
	}

	if *check {
		if err := checkCurrent(outPath, doc, "specification"); err != nil {
			return err
		}
		if err := checkCurrent(sitePath, site, "reference page"); err != nil {
			return err
		}
		fmt.Printf("openapi: %s and %s are current\n", outPath, sitePath)
		return nil
	}

	for _, f := range []struct {
		path string
		data []byte
	}{{outPath, doc}, {sitePath, site}} {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
			return err
		}
		fmt.Printf("openapi: wrote %s (%d bytes)\n", f.path, len(f.data))
	}
	return nil
}

// checkCurrent fails when a generated file on disk differs from what the
// generator would write, naming the file and the command that fixes it.
func checkCurrent(path string, want []byte, what string) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w (run `go run ./cmd/openapi-gen` and commit the result)", path, err)
	}
	if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(want)) {
		return fmt.Errorf(
			"%s is out of date: the API and the %s disagree.\n"+
				"Run `go run ./cmd/openapi-gen` and commit the result.", path, what)
	}
	return nil
}

// repoRoot walks up from the working directory to the repository root, identified
// by .git.
//
// It deliberately does not stop at go.mod: the module root is backend/, while the
// documentation lives at the repository root, so stopping at go.mod would write
// the specification to backend/docs/api/ instead of docs/api/.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no repository root found above the working directory; " +
				"run this from inside the ForgeCRM repository")
		}
		dir = parent
	}
}

// generate builds the document from the real router.
func generate(version string) ([]byte, error) {
	// The router is built from a zero configuration: building it must not
	// require a database or any service, because the route table is static. If
	// that ever stops being true, this command fails loudly rather than
	// emitting a partial document.
	cfg := &config.Config{}
	if version == "" {
		version = "0.1.0"
	}

	srv, err := api.New(&api.Deps{
		Config: cfg,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, ratelimit.NewMemoryStore(), httpx.TrustedProxies{})
	if err != nil {
		return nil, fmt.Errorf("build server: %w", err)
	}

	routes, err := openapi.Routes(srv.Router())
	if err != nil {
		return nil, err
	}

	perms, err := openapi.Permissions(filepath.Join("internal", "api"), filepath.Join("internal", "rbac"))
	if err != nil {
		return nil, err
	}
	perms = openapi.PrefixAPI(perms)

	return openapi.Build(routes, perms, api.APIContract(), openapi.Info{
		Title:       "ForgeCRM API",
		Version:     version,
		Description: openapi.DefaultDescription,
	}, []openapi.Server{
		{URL: "/", Description: "The server this document was generated from."},
	})
}
