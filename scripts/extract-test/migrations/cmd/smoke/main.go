// Command smoke exercises the extracted migrations package: the embedded
// filesystem must carry the SQL, and the up/down pairs must be balanced.
//
// The whole point of this module is that the schema travels inside the binary, so
// the smoke reads the embedded FS rather than the filesystem around it — a build
// that compiles without the SQL would still be useless.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/geff1778/forgecrm/internal/migrations"
)

func main() {
	ups := map[string]bool{}
	downs := map[string]bool{}
	err := fs.WalkDir(migrations.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".up.sql"):
			ups[strings.TrimSuffix(path, ".up.sql")] = true
		case strings.HasSuffix(path, ".down.sql"):
			downs[strings.TrimSuffix(path, ".down.sql")] = true
		}
		return nil
	})
	if err != nil {
		fail("walk embedded migrations: %v", err)
	}
	if len(ups) == 0 {
		fail("the embedded filesystem carries no migrations")
	}
	for name := range ups {
		if !downs[name] {
			fail("migration %s has an up file but no down file", name)
		}
	}
	for name := range downs {
		if !ups[name] {
			fail("migration %s has a down file but no up file", name)
		}
	}
	fmt.Printf("embedded migrations ok: %d up/down pairs\n", len(ups))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
