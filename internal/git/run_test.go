package git

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/gittest"
)

// A process git leaves behind holding its output open does not keep gr waiting for it.
func TestRunStrayChild(t *testing.T) {
	repo := gittest.New(t)
	start := time.Now()
	out, err := run(t.Context(), repo.Dir, nil, "-c", "alias.stray=!sleep 5 & echo done", "stray")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff("done\n", string(out)); diff != "" {
		t.Errorf("output (-want +got):\n%s", diff)
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Errorf("waited %s for git's stray child", took)
	}
}
