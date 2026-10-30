// Command smoke exercises the jobs package's public API in a clean module.
//
// The queue needs a database, so enqueue and claim are not driven here. What is
// driven is the vocabulary a worker's dispatch and an operator's filter are built
// from, and the validation that runs before any write.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/geff1778/forgecrm/internal/jobs"
)

func main() {
	// Every kind is namespaced ("import.execute", "maintenance.retention"). A bare
	// name is the shape a namespaced dispatcher cannot match.
	kinds := []string{
		jobs.KindImportExecute, jobs.KindImportDryRun, jobs.KindExportExecute,
		jobs.KindMigrationRun, jobs.KindWebhookDeliver, jobs.KindEmailSend,
		jobs.KindOutboxDrain, jobs.KindReconcile, jobs.KindRetentionSweep,
		jobs.KindImportTempSweep, jobs.KindAgentSweep,
	}
	seen := map[string]bool{}
	for _, k := range kinds {
		if k == "" || !strings.Contains(k, ".") {
			fail("job kind %q is not namespaced", k)
		}
		if seen[k] {
			fail("job kind %q is declared twice", k)
		}
		seen[k] = true
	}
	fmt.Printf("kinds=%d sample=%s\n", len(kinds), jobs.KindRetentionSweep)

	// The validation runs before the database is touched, so a Queue with no
	// database still rejects a job with no kind. That ordering is the guard.
	q := &jobs.Queue{}
	if _, err := q.Enqueue(context.Background(), jobs.EnqueueRequest{}); err == nil {
		fail("a job with no kind was accepted")
	}
	if _, err := q.EnqueuePeriodic(context.Background(), "", nil); err == nil {
		fail("a periodic job with no kind was accepted")
	}
	fmt.Println("validation runs before any write")

	// The statuses are the vocabulary the queue's filters use.
	for _, s := range []string{jobs.StatusQueued, jobs.StatusRunning, jobs.StatusSucceeded, jobs.StatusFailed, jobs.StatusCancelled} {
		if s == "" {
			fail("an empty status constant")
		}
	}
	fmt.Println("statuses ok")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
