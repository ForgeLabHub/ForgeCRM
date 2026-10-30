// Command smoke exercises the extracted migrate package: the embedded source
// opens and its migration list is complete.
//
// The smoke does not connect to a database — that would need a server and turn a
// standalone check into an integration test. What it proves is that the runner can
// open the embedded migrations it ships with, which is the coupling that breaks
// when the SQL is not carried along.
package main

import (
"fmt"
"os"

"github.com/golang-migrate/migrate/v4/source/iofs"

"github.com/geff1778/forgecrm/internal/migrate"
"github.com/geff1778/forgecrm/internal/migrations"
)

func main() {
_ = migrate.Up // the runner is what the package exists for; referenced, not run
src, err := iofs.New(migrations.FS, ".")
if err != nil {
fail("open the embedded migration source: %v", err)
}
defer src.Close()

// The runner names migrations by their numeric prefix, so the first must be 1.
first, err := src.First()
if err != nil {
fail("the embedded source has no first migration: %v", err)
}
if first != 1 {
fail("the first migration is %d, want 1", first)
}
count := 1
cur := first
for {
next, err := src.Next(cur)
if err != nil {
break
}
count++
cur = next
if count > 1000 {
fail("the migration list does not terminate")
}
}
if count < 2 {
fail("the embedded source carries only %d migration", count)
}
fmt.Printf("embedded migration source ok: %d migrations, first %d\n", count, first)
}

func fail(format string, args ...any) {
fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
os.Exit(1)
}
