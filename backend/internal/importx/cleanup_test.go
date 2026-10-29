package importx

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/config"
)

// TestCleanupTempRemovesOnlyOldExtractionDirectories pins the two properties the
// sweep exists for: an abandoned directory is removed, and a directory young
// enough to belong to an import still in flight is left alone. The second is the
// one that matters: a sweep without an age bound would delete the file a running
// import is reading, and the import would fail part-way through with a parse
// error that says nothing about the real cause.
func TestCleanupTempRemovesOnlyOldExtractionDirectories(t *testing.T) {
	root := t.TempDir()

	mk := func(name string, age time.Duration) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		when := time.Now().Add(-age)
		if err := os.Chtimes(dir, when, when); err != nil {
			t.Fatalf("set mtime on %s: %v", name, err)
		}
		return dir
	}

	abandoned := mk("forgecrm-import-abandoned", 3*time.Hour)
	inFlight := mk("forgecrm-import-running", time.Minute)
	unrelated := mk("some-other-directory", 10*time.Hour)

	svc := &Service{cfg: &config.Config{ImportTempDir: root}}
	removed, err := svc.CleanupTemp(2 * time.Hour)
	if err != nil {
		t.Fatalf("CleanupTemp: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d directories, want 1", removed)
	}
	if _, err := os.Stat(abandoned); !os.IsNotExist(err) {
		t.Errorf("the abandoned directory survived the sweep")
	}
	if _, err := os.Stat(inFlight); err != nil {
		t.Errorf("a directory younger than the age bound was removed: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("a directory the sweep does not own was removed: %v", err)
	}
}

// TestCleanupTempOnAMissingRootIsNotAnError covers the fresh deployment: the
// extraction root has never been created, so there is nothing to sweep. Returning
// an error there would fail the scheduled job on a healthy system.
func TestCleanupTempOnAMissingRootIsNotAnError(t *testing.T) {
	svc := &Service{cfg: &config.Config{ImportTempDir: filepath.Join(t.TempDir(), "never-created")}}
	removed, err := svc.CleanupTemp(time.Hour)
	if err != nil {
		t.Fatalf("CleanupTemp on a missing root returned an error: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed %d directories from a missing root, want 0", removed)
	}
}
