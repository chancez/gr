package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/look"
)

// A change ends when its branch lands. gr sees that itself when the branch's commits are on its
// base, and asks when it cannot tell, as after a squash merge. Once a change has ended, work on
// its branch is a new change's, which gr offers to open.

// openedMsg is a change to show in place of the one shown, as one opened for new work.
type openedMsg struct {
	change store.Change
	status string
	err    error
}

// askAboutLanding asks what the change shown has come to, when its data says there is something to
// ask: whether an emptied change merged, or whether to open a change for a closed one's new work.
// Each is asked once for what it is about, and not over anything else open: until then it is
// asked again, as the TUI ticks.
func (m *model) askAboutLanding() {
	d := m.data
	if d == nil || m.mode != modeNormal {
		return
	}
	if m.asked == nil {
		m.asked = map[string]bool{}
	}
	svc, ctx, c := m.svc, m.ctx, d.change
	switch {
	case d.emptied:
		key := "emptied:" + d.revision.ID
		if m.asked[key] {
			return
		}
		m.asked[key] = true
		state := func(s store.ChangeState) func() tea.Msg {
			return func() tea.Msg {
				return opMsg{status: fmt.Sprintf("change #%d %s", c.Seq, s), err: svc.SetChangeState(ctx, c, s)}
			}
		}
		m.choose(fmt.Sprintf("%s has none of #%d's commits, and %s has not got them: was it merged?", c.Branch, c.Seq, c.BaseBranch),
			"Merged", state(store.ChangeMerged), "Abandoned", state(store.ChangeAbandoned))
	case d.newWork > 0:
		key := "new:" + c.ID + ":" + d.branchHead
		if m.asked[key] {
			return
		}
		m.asked[key] = true
		commits := "commits"
		if d.newWork == 1 {
			commits = "commit"
		}
		m.ask(fmt.Sprintf("%s has %d new %s since #%d %s. Open a new change for them?", c.Branch, d.newWork, commits, c.Seq, c.State),
			m.openNewWork())
	}
}

// openNewWork opens the change for the commits the shown change's branch has had since it closed,
// making it, in place of the closed one.
func (m *model) openNewWork() func() tea.Msg {
	svc, ctx, c := m.svc, m.ctx, m.data.change
	return func() tea.Msg {
		t, err := svc.ParseTarget(ctx, c.Branch, "")
		if err != nil {
			return openedMsg{err: err}
		}
		next, _, err := svc.EnsureChange(ctx, t)
		return openedMsg{change: next, status: fmt.Sprintf("change #%d for %s's new work", next.Seq, c.Branch), err: err}
	}
}

// newWorkLine says, in the overview's details, that the closed change's branch has moved on, with
// the button that opens a change for it: there for as long as that is so, where the question is
// asked once.
func (m *model) newWorkLine() []look.Span {
	d := m.data
	if d == nil || d.newWork == 0 {
		return nil
	}
	c := d.change
	what := string(c.State)
	if c.State == store.ChangeMerged {
		what = "merged into " + c.BaseBranch
	}
	commits := "commits"
	if d.newWork == 1 {
		commits = "commit"
	}
	line := []look.Span{look.Warning(fmt.Sprintf("#%d %s; %s has %d new %s since", c.Seq, what, c.Branch, d.newWork, commits)), look.Default("  ")}
	return append(line, m.controls("pinned", modeNormal, theChange.Target(changeSubject{}), newWorkButton)...)
}
