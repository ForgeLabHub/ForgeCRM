// Command openapi-gen writes the OpenAPI specification for the ForgeCRM API.
//
// It builds the real router — the same value the server serves — walks it for the
// paths and methods, and reads the permission annotations from the route
// registrations in internal/api. The output therefore cannot describe a route
// that does not exist, and cannot omit one that does.
//
// Usage:
//
//	go run ./cmd/openapi-gen              # write docs/api/openapi.json
//	go run ./cmd/openapi-gen -check       # fail if the file is out of date
//	go run ./cmd/openapi-gen -out FILE    # write elsewhere
//
// -check is what CI runs: it regenerates in memory and compares with the
// committed file, so a route added without regenerating the specification fails
// the build rather than shipping a stale document.
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
		"output path; defaults to docs/api/openapi.json at the repository root")
	check := flag.Bool("check", false, "verify the file is current instead of writing it")
	version := flag.String("version", "", "version string for the document; defaults to 0.1.0")
	flag.Parse()

	// The default output is resolved against the repository root rather than the
	// working directory. Running the generator from the repository root instead of
	// backend/ is an easy mistake, and without this it would quietly write to
	// /docs instead of failing.
	outPath := *out
	if outPath == "" {
		root, err := repoRoot()
		if err != nil {
			return err
		}
		outPath = filepath.Join(root, "docs", "api", "openapi.json")
	}

	doc, err := generate(*version)
	if err != nil {
		return err
	}

	if *check {
		existing, err := os.ReadFile(outPath)
		if err != nil {
			return fmt.Errorf("read %s: %w (run `go run ./cmd/openapi-gen` and commit the result)", outPath, err)
		}
		if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(doc)) {
			return fmt.Errorf(
				"%s is out of date: the API and the specification disagree.\n"+
					"Run `go run ./cmd/openapi-gen` and commit the result.", outPath)
		}
		fmt.Printf("openapi: %s is current\n", outPath)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, doc, 0o644); err != nil {
		return err
	}
	fmt.Printf("openapi: wrote %s (%d bytes)\n", outPath, len(doc))
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
