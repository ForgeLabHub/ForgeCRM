// Command smoke exercises the extracted migratex package: the implemented source
// kinds are named explicitly, and the unimplemented ones are refused rather than
// accepted and then failing at connect time.
package main

import (
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/migratex"
)

func main() {
	for _, k := range []migratex.SourceKind{
		migratex.SourcePostgres, migratex.SourceSQLite, migratex.SourceREST,
	} {
		if !migratex.Implemented(k) {
			fail("source kind %q is not implemented", k)
		}
	}
	for _, k := range []migratex.SourceKind{
		migratex.SourceMySQL, migratex.SourceMSSQL, migratex.SourceOracle,
	} {
		if migratex.Implemented(k) {
			fail("source kind %q claims to be implemented", k)
		}
	}
	if migratex.Implemented("nonsense") {
		fail("an unknown source kind was reported as implemented")
	}
	fmt.Println("source kind catalog ok")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
