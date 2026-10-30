package jobs

import (
	"context"
	"testing"
	"time"
)

// The status and kind vocabularies are the contract the worker's dispatch and an
// operator's filter are built from. A duplicate, an empty value or a kind that is
// not namespaced ("import.execute") would be accepted at the write and then
// match nothing at the read — the same class of bug the outbox vocabulary had.
func TestStatusAndKindVocabulariesAreWellFormed(t *testing.T) {
	statuses := []string{StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled}
	seen := map[string]bool{}
	for _, s := range statuses {
		if s == "" {
			t.Error("a status constant is empty")
			continue
		}
		if seen[s] {
			t.Errorf("status %q is declared twice", s)
		}
		seen[s] = true
	}

	kinds := []string{
		KindImportExecute, KindImportDryRun, KindExportExecute, KindMigrationRun,
		KindWebhookDeliver, KindEmailSend, KindOutboxDrain, KindReconcile,
		KindRetentionSweep, KindImportTempSweep, KindImportFileReclaim, KindAgentSweep,
		KindStorageReconcile,
	}
	seenKind := map[string]bool{}
	for _, k := range kinds {
		if k == "" {
			t.Error("a kind constant is empty")
			continue
		}
		// Every kind is namespaced ("import.execute", "maintenance.retention"). A bare
		// name is the shape that cannot be matched by a namespaced dispatcher.
		if !containsDot(k) {
			t.Errorf("job kind %q is not namespaced", k)
		}
		if seenKind[k] {
			t.Errorf("job kind %q is declared twice", k)
		}
		seenKind[k] = true
	}
}

func containsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}

// Enqueue rejects an empty kind before it touches the database. That ordering is
// what keeps an unattributable, undispatchable job out of the queue, and it is
// testable with no server.
func TestEnqueueRequiresKind(t *testing.T) {
	q := &Queue{}
	if _, err := q.Enqueue(context.Background(), EnqueueRequest{}); err == nil {
		t.Error("a job with no kind was accepted")
	}
	if _, err := q.EnqueuePeriodic(context.Background(), "", nil); err == nil {
		t.Error("a periodic job with no kind was accepted")
	}
}

// backoffFor grows with the attempt number and is capped, so a permanently broken
// job does not push its next attempt beyond a bounded horizon.
func TestBackoffForGrowsAndCaps(t *testing.T) {
	if got := backoffFor(0); got != time.Second {
		t.Errorf("backoffFor(0) = %s, want 1s", got)
	}
	if got := backoffFor(3); got != 8*time.Second {
		t.Errorf("backoffFor(3) = %s, want 8s", got)
	}
	// The delay grows until the 30m ceiling is reached and then stays there. The
	// clamp on the shift must sit above the ceiling, or the ceiling is unreachable
	// and the delay stops growing short of it — which is exactly the bug this test
	// caught when it was written.
	if got := backoffFor(10); got != 1024*time.Second {
		t.Errorf("backoffFor(10) = %s, want 1024s", got)
	}
	for _, attempt := range []int{11, 12, 63, 1000} {
		if got := backoffFor(attempt); got != 30*time.Minute {
			t.Errorf("backoffFor(%d) = %s, want the 30m cap", attempt, got)
		}
	}
}

func TestMinHelper(t *testing.T) {
	if min(2, 5) != 2 || min(5, 2) != 2 || min(3, 3) != 3 {
		t.Error("min returned the wrong value")
	}
}

// nonNil keeps a JSON column an object rather than SQL NULL, so a reader never has
// to handle both an absent key and a null document.
func TestNonNilPayload(t *testing.T) {
	if got := nonNil(nil); got == nil {
		t.Fatal("nonNil(nil) returned nil")
	}
	if m, ok := nonNil(nil).(map[string]any); !ok || len(m) != 0 {
		t.Fatalf("nonNil(nil) = %v, want an empty map", nonNil(nil))
	}
	in := map[string]any{"k": "v"}
	if got := nonNil(in); got == nil {
		t.Fatal("nonNil dropped a non-nil value")
	}
}
