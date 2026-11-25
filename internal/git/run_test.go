package git

import (
	"errors"
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

// A git command running past the timeout is killed, and says so.
func TestRunTimeout(t *testing.T) {
	repo := gittest.New(t)
	SetTimeout(100 * time.Millisecond)
	t.Cleanup(func() { SetTimeout(DefaultTimeout) })
	start := time.Now()
	_, err := run(t.Context(), repo.Dir, nil, "-c", "alias.slow=!sleep 5", "slow")
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("err = %v, want ErrTimeout", err)
	}
	if diff := cmp.Diff("git -c alias.slow=!sleep 5 slow: timed out after 100ms, git.timeout in the config file", err.Error()); diff != "" {
		t.Errorf("message (-want +got):\n%s", diff)
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Errorf("killed after %s", took)
	}
}
