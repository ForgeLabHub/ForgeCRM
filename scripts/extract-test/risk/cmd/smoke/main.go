// Command smoke exercises the extracted risk package: the threshold and signal
// types are the contract the API returns, and a zero threshold is zero.
package main

import (
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/risk"
)

func main() {
	var th risk.Thresholds
	if th.StalledDealDays != 0 {
		fail("a zero threshold is not zero")
	}
	var sig risk.Signal
	_ = sig
	fmt.Println("risk thresholds and signal types ok")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
