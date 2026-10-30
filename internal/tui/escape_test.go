package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// evil is text a terminal would act on: blink, a window title, and a bell. gr draws none of these
// itself, so finding one in the output means text from the repository or a review got through.
const evil = "\x1b[5m\x1b]0;pwned\x07"

// TestControlCharactersAreShown puts control characters in everything gr draws that comes from
// the repository or a review, and checks every screen shows them rather than sending them.
func TestControlCharactersAreShown(t *testing.T) {
	e := newTUIEnv(t)
	ctx := t.Context()
	e.repo.Write("evil"+evil+".txt", gittest.Lines("line "+evil, "\tline\r"))
	e.repo.Commit("subject " + evil + "\n\nbody " + evil)
	e.repo.Git("commit", "-q", "--amend", "--no-edit", "--author=Name"+evil+" <e"+evil+"@example.com>")
	if _, _, err := e.svc.Sync(ctx, e.chg); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Tx(ctx, func(tx *store.Tx) error { return tx.SetChangeTitle(ctx, e.chg.ID, "title "+evil) }); err != nil {
		t.Fatal(err)
	}
	other, err := review.Open(ctx, e.store, e.svc.Git(), store.Author{Name: "Reviewer" + evil})
	if err != nil {
		t.Fatal(err)
	}
	open, err := other.StartThread(ctx, e.chg, review.NewThread{Scope: store.ScopeDiff, Path: "evil" + evil + ".txt",
		StartLine: 1, Body: "comment " + evil})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := other.StartThread(ctx, e.chg, review.NewThread{Scope: store.ScopeChange, Body: "resolved " + evil})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Submit(ctx, e.chg, store.VerdictComment, "summary "+evil); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Reply(ctx, e.chg, open.ID, "reply "+evil, false); err != nil {
		t.Fatal(err)
	}
	if err := other.SetThreadState(ctx, resolved.ID, store.ThreadResolved); err != nil {
		t.Fatal(err)
	}
	e.m.Update(tea.WindowSizeMsg{Width: 160, Height: 80})
	e.drain(e.m.loadCmd())

	check := func(name string) {
		t.Helper()
		out := e.m.render()
		for _, bad := range []string{"\x1b[5m", "\x1b]", "\x07", "\r"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s: %q reached the terminal:\n%s", name, bad, e.screen())
			}
		}
		if title := e.m.View().WindowTitle; strings.ContainsAny(title, "\x1b\x07") {
			t.Errorf("%s: window title %q", name, title)
		}
	}
	e.openNav("Overview")
	check("overview")
	if !strings.Contains(e.screen(), "comment ^[[5m^[]0;pwned^G") {
		t.Errorf("control characters not shown in caret notation:\n%s", e.screen())
	}
	e.openNav("subject")
	check("commit")
	e.openNav("All changes")
	check("whole diff")
	e.press("s")
	check("split")
}
