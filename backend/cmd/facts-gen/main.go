// Package main implements facts-gen: the reproducible generator behind
// docs/verification/project-facts.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	repo := flag.String("repo", ".", "repository root")
	check := flag.Bool("check", false, "fail if project-facts.md is out of date or data is inconsistent")
	write := flag.Bool("write", false, "update the FACTS:AUTO block in docs/verification/project-facts.md in place, preserving human prose")
	out := flag.String("out", "", "write JSON metrics to this file instead of stdout")
	flag.Parse()

	rep, err := Compute(*repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "facts-gen:", err)
		os.Exit(1)
	}
	if !rep.Consistency.RbacConstsMatchCatalog {
		fmt.Fprintln(os.Stderr, "facts-gen: RBAC constants and catalog map disagree")
		os.Exit(1)
	}
	if !rep.Consistency.RbacSeededMatchCatalog {
		fmt.Fprintln(os.Stderr, "facts-gen: seeded permissions and RBAC catalog disagree")
		os.Exit(1)
	}
	if !rep.Consistency.MigrationPairsSymmetric {
		fmt.Fprintln(os.Stderr, "facts-gen: up/down migration counts are asymmetric")
		os.Exit(1)
	}
	if !rep.Consistency.RoutePathsUnique {
		fmt.Fprintln(os.Stderr, "facts-gen: duplicate route paths in App.tsx")
		os.Exit(1)
	}

	if *out != "" {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "facts-gen:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "facts-gen:", err)
			os.Exit(1)
		}
		return
	}

	md := RenderMarkdown(rep, commitFromEnv())
	if *check {
		path := filepath.Join(*repo, "docs/verification/project-facts.md")
		cur, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "facts-gen -check: %v\n", err)
			os.Exit(1)
		}
		if !MetricsBlockMatch(string(cur), md) {
			fmt.Fprintln(os.Stderr, "facts-gen -check: блок FACTS:AUTO в docs/verification/project-facts.md разошёлся с кодом или с HEAD; перегенерируйте его (go run ./cmd/facts-gen -write)")
			os.Exit(1)
		}
		fmt.Println("project-facts.md соответствует коду")
		return
	}
	if *write {
		path := filepath.Join(*repo, "docs/verification/project-facts.md")
		cur, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "facts-gen -write: %v\n", err)
			os.Exit(1)
		}
		updated, err := ReplaceBlock(string(cur), md)
		if err != nil {
			fmt.Fprintf(os.Stderr, "facts-gen -write: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "facts-gen -write: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("project-facts.md обновлён")
		return
	}
	fmt.Print(md)
}

// commitFromEnv reports the commit the facts describe. CI passes the checked-out
// SHA via FACTS_COMMIT so the stamp inside the auto block is verifiable; locally
// we shell out to git rev-parse HEAD. If neither works the stamp is explicit.
func commitFromEnv() string {
	if c := os.Getenv("FACTS_COMMIT"); c != "" {
		return c
	}
	if c, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		return strings.TrimSpace(string(c))
	}
	return "(SHA недоступен)"
}
