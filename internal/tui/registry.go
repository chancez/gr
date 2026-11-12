package tui

import (
	"errors"
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/interact"
)

// registry.go is the TUI's side of ADR 10's registry: its subjects, their kinds, actions, and keys,
// and its places. Only this file makes kinds and places.
//
// A kind is what a subject is: a commit, a file in a commit, a line, a fold of unchanged lines.
// Where it is belongs to its place, as a sidebar entry opens in the main pane, and what state it is
// in to its actions' Applies, as only your own comment can be edited.

// Subjects.

// inView is the view a subject is in, by its index in m.views.
type inView struct {
	at   int
	view view
}

func (v inView) shown() inView { return v }

// viewLike is a subject that is a view, or is in one.
type viewLike interface{ shown() inView }

func (m *model) inView(i int) inView { return inView{at: i, view: m.views[i]} }

// The views, by what each is about. A commit's view is the commit itself.
type overviewSubject struct{ inView }
type diffSubject struct{ inView }
type interdiffSubject struct{ inView }
type commitSubject struct{ inView }

// changeSubject is the change under review.
type changeSubject struct{}

// fileSubject is a file of a view, in the sidebar's file list or as its header in a diff.
type fileSubject struct {
	inView
	file int
	diff git.FileDiff
}

// rowSubject is a row of a diff: a line, a hunk's header, or a fold of unchanged lines, with its
// file, and in split mode the side it is on.
type rowSubject struct {
	inView
	index   int
	side    anchor.Side
	file    fileSubject
	hasFile bool
	row     row
}

// threadSubject is a thread, in a diff or the overview, with its file in a diff, or a review's
// summary.
type threadSubject struct {
	inView
	index   int
	file    fileSubject
	hasFile bool
	thread  review.ThreadView
}

// reviewSubject is a review's bar in the overview, with its summary thread if it has one.
type reviewSubject struct {
	inView
	index   int
	review  store.Review
	summary *review.ThreadView
}

// paneSubject is a pane, for the moves that take the cursor through it.
type paneSubject struct{}

// appSubject is gr itself, for commands that act on no subject.
type appSubject struct{}

// composeSubject is the comment box that is open, verdictSubject the verdict prompt, and
// confirmSubject the y/n prompt.
type composeSubject struct{}
type verdictSubject struct{}
type confirmSubject struct{}

// fileOf is file f of view v, if there is one.
func (m *model) fileOf(v inView, f int) (fileSubject, bool) {
	if f < 0 || f >= len(v.view.files) {
		return fileSubject{}, false
	}
	return fileSubject{inView: v, file: f, diff: v.view.files[f]}, true
}

// do adapts a function that changes the model without a command.
func do[S any](f func(m *model)) func(*model, S) tea.Cmd {
	return func(m *model, _ S) tea.Cmd { f(m); return nil }
}

// cmd adapts a function that returns a command.
func cmd[S any](f func(m *model) tea.Cmd) func(*model, S) tea.Cmd {
	return func(m *model, _ S) tea.Cmd { return f(m) }
}

// refuse is an action that only says why a key does nothing here, as commenting in the read-only
// interdiff. Kinds BindQuiet it, so it stays out of menus.
func refuse[S any](why string) *interact.Action[*model, S] {
	return &interact.Action[*model, S]{
		Title: why,
		Run:   func(m *model, _ S) tea.Cmd { m.setError(errors.New(why)); return nil },
	}
}

// Views: shown when the sidebar's cursor lands on one, and opened from it.

func showView[S viewLike]() *interact.Action[*model, S] {
	return &interact.Action[*model, S]{
		Title: "show",
		Run:   func(m *model, s S) tea.Cmd { m.setView(s.shown().at); return nil },
	}
}

func openView[S viewLike]() *interact.Action[*model, S] {
	return &interact.Action[*model, S]{
		Title: "open",
		Run: func(m *model, s S) tea.Cmd {
			m.setView(s.shown().at)
			m.setFocus(focusMain)
			return nil
		},
	}
}

var (
	actShowOverview  = showView[overviewSubject]()
	actOpenOverview  = openView[overviewSubject]()
	actShowDiff      = showView[diffSubject]()
	actOpenDiff      = openView[diffSubject]()
	actShowInterdiff = showView[interdiffSubject]()
	actOpenInterdiff = openView[interdiffSubject]()
	actShowCommit    = showView[commitSubject]()
	actOpenCommit    = openView[commitSubject]()
	actGoToCommit    = &interact.Action[*model, commitSubject]{
		Title:   "open",
		Applies: func(m *model, s commitSubject) bool { return s.at != m.current },
		Run: func(m *model, s commitSubject) tea.Cmd {
			m.setView(s.at)
			m.setFocus(focusMain)
			return nil
		},
	}
	actFilterThreads = &interact.Action[*model, overviewSubject]{
		Title: "filter",
		Run: func(m *model, _ overviewSubject) tea.Cmd {
			m.filter = (m.filter + 1) % filterCount
			m.rebuildMain()
			m.setStatus("showing %s threads", m.filter)
			return nil
		},
	}
)

// The change and its commits.
var (
	actCommentOnChange = &interact.Action[*model, changeSubject]{
		Title: "comment",
		Help:  "comment on the change",
		Run:   cmd[changeSubject](func(m *model) tea.Cmd { return m.commentOnChange() }),
	}
	actOpenNewWork = &interact.Action[*model, changeSubject]{
		Title:   "new change",
		Help:    "open a change for the branch's commits since this one closed",
		Applies: func(m *model, _ changeSubject) bool { return m.data != nil && m.data.newWork > 0 },
		Run:     func(m *model, _ changeSubject) tea.Cmd { return m.openNewWork() },
	}
	actSubmit = &interact.Action[*model, changeSubject]{
		Title: "submit",
		Help:  "submit your review: then a, r, or c for a verdict",
		Run:   do[changeSubject](func(m *model) { m.mode = modeVerdict }),
	}
	actDiscardDraft = &interact.Action[*model, changeSubject]{
		Title:   "discard draft",
		Help:    "discard your draft review",
		Applies: func(m *model, _ changeSubject) bool { _, ok := m.data.draft(); return ok },
		Run:     cmd[changeSubject](func(m *model) tea.Cmd { return m.discardDraft() }),
	}
	actCommentOnCommit = &interact.Action[*model, commitSubject]{
		Title: "comment",
		Help:  "comment on the commit",
		Run:   func(m *model, s commitSubject) tea.Cmd { return m.commentOnCommit(s.view) },
	}
	actMarkCommitReviewed = &interact.Action[*model, commitSubject]{
		Title: "mark reviewed",
		Help:  "mark the commit reviewed",
		Run:   func(m *model, s commitSubject) tea.Cmd { return m.toggleCommitReviewed(s.view) },
	}
)

// canWrite applies where the subject's view takes comments and marks: everywhere but the
// interdiff, whose lines are between two revisions, with none to anchor a thread to.
func canWrite[S viewLike](_ *model, s S) bool { return !s.shown().view.readOnly() }

// writable binds keys to a, which applies where canWrite does, and to a refusal saying why not
// where it does not, which keeps the keys from reaching what the subject carries.
func writable[S viewLike](k *interact.Kind[*model, S], a *interact.Action[*model, S], help, desc string, keys ...string) {
	k.Bind(a, help, desc, keys...)
	refusal := refuse[S]("the interdiff is read-only; comment on the files or commits")
	refusal.Applies = func(m *model, s S) bool { return !canWrite(m, s) }
	k.BindQuiet(refusal, keys...)
}

// Files, the same in a commit, the whole diff, and the interdiff, where they are read-only.
var (
	actShowFile = &interact.Action[*model, fileSubject]{
		Title: "show",
		Run:   func(m *model, s fileSubject) tea.Cmd { m.showFile(s); return nil },
	}
	actOpenFile = &interact.Action[*model, fileSubject]{
		Title: "open",
		Run:   func(m *model, s fileSubject) tea.Cmd { m.showFile(s); m.setFocus(focusMain); return nil },
	}
	// The file list's names scroll sideways, the start of a long one cut at the front into view.
	actScrollNamesBack = &interact.Action[*model, fileSubject]{
		Title: "scroll names left",
		Run:   func(m *model, _ fileSubject) tea.Cmd { m.filesNav.scrollSide(sideStep); return nil },
	}
	actScrollNamesOn = &interact.Action[*model, fileSubject]{
		Title: "scroll names right",
		Run:   func(m *model, _ fileSubject) tea.Cmd { m.filesNav.scrollSide(-sideStep); return nil },
	}
	actFoldFile = &interact.Action[*model, fileSubject]{
		Title: "fold",
		Help:  "collapse or expand the file",
		Run:   func(m *model, s fileSubject) tea.Cmd { m.toggleCollapsed(s.view, s.file); return nil },
	}
	actMarkFileReviewed = &interact.Action[*model, fileSubject]{
		Title:   "mark reviewed",
		Help:    "mark the file reviewed",
		Applies: canWrite[fileSubject],
		Run:     func(m *model, s fileSubject) tea.Cmd { return m.toggleFileReviewed(s.view, s.file) },
	}
	actCommentOnFile = &interact.Action[*model, fileSubject]{
		Title:   "comment",
		Help:    "comment on file",
		Applies: canWrite[fileSubject],
		Run:     func(m *model, s fileSubject) tea.Cmd { return m.commentOnFile(s.view, s.file) },
	}
)

// Opening what the cursor is on in $EDITOR, as it is in the view.
var (
	actOpenFileInEditor = &interact.Action[*model, fileSubject]{
		Title: "open in $EDITOR",
		Help:  "open the file in $EDITOR, read-only, as it is here",
		Run: func(m *model, s fileSubject) tea.Cmd {
			line := 1
			if len(s.diff.Hunks) > 0 {
				line = s.diff.Hunks[0].NewStart
			}
			return m.openFileCmd(s.view, s.file, anchor.SideNew, line)
		},
	}
	actOpenRowInEditor = &interact.Action[*model, rowSubject]{
		Title:   "open in $EDITOR",
		Help:    "open the file in $EDITOR at this line, read-only, as it is here",
		Applies: func(m *model, s rowSubject) bool { return s.hasFile },
		Run: func(m *model, s rowSubject) tea.Cmd {
			side, line := openingLine(s)
			return m.openFileCmd(s.view, s.file.file, side, line)
		},
	}
)

// Rows of a diff: lines, hunk headers, and folds of unchanged lines.
var (
	actCommentOnLine = &interact.Action[*model, rowSubject]{
		Title:   "comment",
		Help:    "comment on the line or selection",
		Applies: canWrite[rowSubject],
		Run:     func(m *model, s rowSubject) tea.Cmd { return m.commentOnRow(s.index, s.side, false) },
	}
	actSelectLine = &interact.Action[*model, rowSubject]{
		Title: "select",
		Help:  "start or end a line selection",
		// A selection runs from the cursor, so only the cursor's row starts or ends one.
		Applies: func(m *model, s rowSubject) bool { return s.index == m.main.cursor },
		Run:     func(m *model, _ rowSubject) tea.Cmd { m.toggleSelection(); return nil },
	}
	actSearchText = &interact.Action[*model, rowSubject]{
		Title: "search for the selected text",
		Help:  "search for the text selected with the mouse",
		// A match is within a line, so the text must be too.
		Applies: func(m *model, s rowSubject) bool {
			lo, hi, ok := m.textLines()
			return ok && lo == hi && s.index == lo
		},
		Run: func(m *model, _ rowSubject) tea.Cmd { m.searchText(); return nil },
	}
	actExpandFold = &interact.Action[*model, rowSubject]{
		Title: "expand",
		Help:  "show more of the unchanged lines, above the hunk below",
		Run: func(m *model, s rowSubject) tea.Cmd {
			// Toward the hunk below, the context most often wanted; after the last hunk, down.
			r := m.main.rows[s.index]
			m.widenGap(r, r.gap.end == 0)
			return nil
		},
	}
	actExpandUp = &interact.Action[*model, rowSubject]{
		Title:   "expand up",
		Help:    fmt.Sprintf("show %d more unchanged lines, above the hunk below", gapStep),
		Applies: func(m *model, s rowSubject) bool { return gapWidens(m.main.rows[s.index], false) },
		Run:     func(m *model, s rowSubject) tea.Cmd { m.widenGap(m.main.rows[s.index], false); return nil },
	}
	actExpandDown = &interact.Action[*model, rowSubject]{
		Title:   "expand down",
		Help:    fmt.Sprintf("show %d more unchanged lines, below the hunk above", gapStep),
		Applies: func(m *model, s rowSubject) bool { return gapWidens(m.main.rows[s.index], true) },
		Run:     func(m *model, s rowSubject) tea.Cmd { m.widenGap(m.main.rows[s.index], true); return nil },
	}
	actExpandAll = &interact.Action[*model, rowSubject]{
		Title: "expand all",
		Help:  "show all the unchanged lines",
		Run:   func(m *model, s rowSubject) tea.Cmd { m.setGap(m.main.rows[s.index], gapShown{All: true}); return nil },
	}
	actCollapseGap = &interact.Action[*model, rowSubject]{
		Title: "collapse",
		Help:  "hide the unchanged lines again",
		Applies: func(m *model, s rowSubject) bool {
			r := m.main.rows[s.index]
			return r.gap != nil && m.openGaps[gapKey(m.views[m.current], m.views[m.current].files[r.file].Path(), r.gap.index)] != (gapShown{})
		},
		Run: func(m *model, s rowSubject) tea.Cmd { m.setGap(m.main.rows[s.index], gapShown{}); return nil },
	}
	actNoCommentOnFold = refuse[rowSubject]("unchanged lines outside the diff cannot be commented on yet")
)

// gapWidens reports whether fold row r opens a step more from its top, when down, or its bottom: a
// gap has no hunk above to open down from before the first, nor one below after the last, and one
// with a step or less hidden opens all at once instead.
func gapWidens(r row, down bool) bool {
	if r.kind != rowGap {
		return false
	}
	if r.gap.count > 0 && r.gap.count <= gapStep || r.gap.end != 0 && r.gap.end-r.gap.start+1 <= gapStep {
		return false
	}
	if down {
		return r.gap.index > 0
	}
	return r.gap.end != 0
}

// ownsComment applies to threads with a comment of yours.
func ownsComment(m *model, s threadSubject) bool {
	_, ok := m.lastOwnComment(s.thread)
	return ok
}

// Threads, the same in a diff, the overview, and a review's summary, which the overview can also
// open where they are.
var (
	actToggleThread = &interact.Action[*model, threadSubject]{
		Title: "expand",
		Help:  "expand or collapse the thread",
		Run:   func(m *model, s threadSubject) tea.Cmd { m.foldRow(s.index, foldToggle); return nil },
	}
	actReplyToThread = &interact.Action[*model, threadSubject]{
		Title: "reply",
		Run: func(m *model, s threadSubject) tea.Cmd {
			return m.threadActionOn(s.thread, s.index == m.main.cursor, composeReply)
		},
	}
	actEditComment = &interact.Action[*model, threadSubject]{
		Title:   "edit",
		Help:    "edit your comment",
		Applies: ownsComment,
		Run: func(m *model, s threadSubject) tea.Cmd {
			return m.threadActionOn(s.thread, s.index == m.main.cursor, composeEdit)
		},
	}
	actResolveThread = &interact.Action[*model, threadSubject]{
		Title: "resolve",
		Help:  "resolve or reopen the thread",
		Run: func(m *model, s threadSubject) tea.Cmd {
			state := store.ThreadResolved
			if s.thread.State == store.ThreadResolved {
				state = store.ThreadOpen
			}
			return m.setThreadState(s.thread, state)
		},
	}
	actAddressThread = &interact.Action[*model, threadSubject]{
		Title: "addressed",
		Help:  "mark the thread addressed, or open again an addressed one",
		Run: func(m *model, s threadSubject) tea.Cmd {
			state := store.ThreadAddressed
			if s.thread.State == store.ThreadAddressed {
				state = store.ThreadOpen
			}
			return m.setThreadState(s.thread, state)
		},
	}
	actDeleteComment = &interact.Action[*model, threadSubject]{
		Title:   "delete",
		Help:    "delete your comment, or the thread you started",
		Applies: ownsComment,
		Run: func(m *model, s threadSubject) tea.Cmd {
			m.deleteOwnComment(s.thread, s.index == m.main.cursor)
			return nil
		},
	}
	actOpenThreadInDiff = &interact.Action[*model, threadSubject]{
		Title: "open in diff",
		Help:  "open the thread in the whole diff",
		// A thread on the whole change has no code to open.
		Applies: func(m *model, s threadSubject) bool { return s.thread.Location.Scope != store.ScopeChange },
		Run:     func(m *model, s threadSubject) tea.Cmd { m.openThread(s.thread, true); return nil },
	}
	actOpenThreadInCommit = &interact.Action[*model, threadSubject]{
		Title:   "open in commit",
		Help:    "open the thread in its commit",
		Applies: func(m *model, s threadSubject) bool { return s.thread.Location.Scope == store.ScopeCommit },
		Run:     func(m *model, s threadSubject) tea.Cmd { m.openThread(s.thread, false); return nil },
	}
)

// Reviews.
var (
	actToggleReview = &interact.Action[*model, reviewSubject]{
		Title:   "summary",
		Help:    "expand or collapse the summary",
		Applies: func(m *model, s reviewSubject) bool { return s.summary != nil },
		Run:     func(m *model, s reviewSubject) tea.Cmd { m.foldRow(s.index, foldToggle); return nil },
	}
	actDeleteReview = &interact.Action[*model, reviewSubject]{
		Title:   "delete",
		Help:    "delete your review",
		Applies: func(m *model, s reviewSubject) bool { return s.review.State == store.ReviewSubmitted },
		Run:     func(m *model, s reviewSubject) tea.Cmd { return m.deleteReview(s.review) },
	}
)

// Panes: each has moves of its own, from the same keys.

// paneAction is an action on the pane focus is in.
func paneAction(title string, run func(m *model) tea.Cmd) *interact.Action[*model, paneSubject] {
	return &interact.Action[*model, paneSubject]{Title: title, Run: cmd[paneSubject](run)}
}

// paneMoves are the moves through a pane, as actions.
type paneMoves struct {
	up, down, halfUp, halfDown, pageUp, pageDown, top, bottom *interact.Action[*model, paneSubject]
}

func movesOf(mv moves) paneMoves {
	return paneMoves{
		up:       paneAction("up", func(m *model) tea.Cmd { return mv.by(m, -1) }),
		down:     paneAction("down", func(m *model) tea.Cmd { return mv.by(m, 1) }),
		halfUp:   paneAction("half page up", func(m *model) tea.Cmd { return mv.page(m, -1, 2) }),
		halfDown: paneAction("half page down", func(m *model) tea.Cmd { return mv.page(m, 1, 2) }),
		pageUp:   paneAction("page up", func(m *model) tea.Cmd { return mv.page(m, -1, 1) }),
		pageDown: paneAction("page down", func(m *model) tea.Cmd { return mv.page(m, 1, 1) }),
		top:      paneAction("top", func(m *model) tea.Cmd { return mv.end(m, false) }),
		bottom:   paneAction("bottom", func(m *model) tea.Cmd { return mv.end(m, true) }),
	}
}

func (p paneMoves) actions() []*interact.Action[*model, paneSubject] {
	return []*interact.Action[*model, paneSubject]{p.up, p.down, p.halfUp, p.halfDown, p.pageUp, p.pageDown, p.top, p.bottom}
}

// bindMoves binds the keys every pane moves by: j and k, the arrows, pages, and the ends.
func bindMoves(k *interact.Kind[*model, paneSubject], p paneMoves) {
	k.Bind(p.down, "j/k", "down/up", "down", "j")
	k.Bind(p.up, "", "", "up", "k")
	k.Bind(p.halfDown, "d/u", "half page down/up", "ctrl+d", "d")
	k.Bind(p.halfUp, "", "", "ctrl+u", "u")
	k.Bind(p.pageDown, "f/b", "page down/up", "pgdown", "ctrl+f", "f", "space")
	k.Bind(p.pageUp, "", "", "pgup", "ctrl+b", "b")
	k.Bind(p.top, "g/G", "top/bottom", "g", "home")
	k.Bind(p.bottom, "", "", "G", "end")
}

// jumpTo moves the cursor to the next row in direction dir that matches.
func jumpTo(title string, dir int, match func(row) bool) *interact.Action[*model, paneSubject] {
	return paneAction(title, func(m *model) tea.Cmd { m.jumpTo(dir, match); m.syncNav(); return nil })
}

// jumpToThread moves to the next thread in direction dir, as a jump ctrl+o returns from.
func jumpToThread(title string, dir int) *interact.Action[*model, paneSubject] {
	return paneAction(title, func(m *model) tea.Cmd {
		m.jump(func() { m.jumpTo(dir, func(r row) bool { return r.kind == rowThread }) })
		m.syncNav()
		return nil
	})
}

var (
	sidebarMoves  = movesOf(listMoves)
	diffMovesOf   = movesOf(diffMoves)
	readerMovesOf = movesOf(readerMoves)

	actToSidebar = paneAction("sidebar", func(m *model) tea.Cmd { m.focusSidebar(); return nil })
	actMoveLeft  = paneAction("sidebar, or the old side in split", func(m *model) tea.Cmd { m.moveLeft(); return nil })
	actMoveRight = paneAction("the new side in split", func(m *model) tea.Cmd { m.main.side = anchor.SideNew; return nil })

	actNextFile   = jumpTo("next file", 1, func(r row) bool { return r.kind == rowFile })
	actPrevFile   = jumpTo("previous file", -1, func(r row) bool { return r.kind == rowFile })
	actNextHunk   = jumpTo("next hunk", 1, func(r row) bool { return r.kind == rowHunk })
	actPrevHunk   = jumpTo("previous hunk", -1, func(r row) bool { return r.kind == rowHunk })
	actNextChange = paneAction("next change", func(m *model) tea.Cmd { m.jumpToIndex(1, m.isChangeStart); m.syncNav(); return nil })
	actPrevChange = paneAction("previous change", func(m *model) tea.Cmd { m.jumpToIndex(-1, m.isChangeStart); m.syncNav(); return nil })

	actNextThreadInDiff     = jumpToThread("next thread", 1)
	actPrevThreadInDiff     = jumpToThread("previous thread", -1)
	actNextThreadInOverview = jumpToThread("next thread", 1)
	actPrevThreadInOverview = jumpToThread("previous thread", -1)

	actNextUnresolved = paneAction("next unresolved thread", func(m *model) tea.Cmd { m.stepUnresolved(1); return nil })
	actPrevUnresolved = paneAction("previous unresolved thread", func(m *model) tea.Cmd { m.stepUnresolved(-1); return nil })

	actSelectLines = paneAction("select lines", func(m *model) tea.Cmd { m.toggleSelection(); return nil })
	actFoldPrefix  = paneAction("folds", func(m *model) tea.Cmd {
		// What the next key does shows in a box of its own, as prefixHints draws it.
		m.pending = "z"
		return nil
	})
)

// appAction is a command on gr itself.
func appAction(title string, run func(m *model) tea.Cmd) *interact.Action[*model, appSubject] {
	return &interact.Action[*model, appSubject]{Title: title, Run: cmd[appSubject](run)}
}

// actGoTab goes to tabs 1 to 9, as alt and a digit do.
var actGoTab = func() (out [9]*interact.Action[*model, appSubject]) {
	for i := range out {
		n := i
		out[i] = appAction("tab "+strconv.Itoa(n+1), func(*model) tea.Cmd { return tabCmd(goTabMsg{n: n}) })
	}
	return out
}()

// loadedApp is a command that needs the change loaded.
func loadedApp(title string, run func(m *model) tea.Cmd) *interact.Action[*model, appSubject] {
	a := appAction(title, run)
	a.Applies = func(m *model, _ appSubject) bool { return m.data != nil }
	return a
}

// Commands on gr itself.
var (
	actQuit        = appAction("quit", func(*model) tea.Cmd { return tea.Quit })
	actNewTab      = appAction("new tab", func(m *model) tea.Cmd { return m.pickTab() })
	actCloseTab    = appAction("close tab", func(*model) tea.Cmd { return tabCmd(closeTabMsg{}) })
	actNextTab     = appAction("next tab", func(*model) tea.Cmd { return tabCmd(stepTabMsg{dir: 1}) })
	actPrevTab     = appAction("previous tab", func(*model) tea.Cmd { return tabCmd(stepTabMsg{dir: -1}) })
	actTabRight    = appAction("move tab right", func(*model) tea.Cmd { return tabCmd(moveTabMsg{dir: 1}) })
	actTabLeft     = appAction("move tab left", func(*model) tea.Cmd { return tabCmd(moveTabMsg{dir: -1}) })
	actShowHelp    = appAction("help", func(m *model) tea.Cmd { m.mode = modeHelp; return nil })
	actCancel      = appAction("clear the search or selection", func(m *model) tea.Cmd { m.clearTransient(); return nil })
	actNextPane    = appAction("next pane", func(m *model) tea.Cmd { m.cycleFocus(1); return nil })
	actPrevPane    = appAction("previous pane", func(m *model) tea.Cmd { m.cycleFocus(-1); return nil })
	actNextList    = appAction("files list", func(m *model) tea.Cmd { m.focusSection(1); return nil })
	actPrevList    = appAction("changes list", func(m *model) tea.Cmd { m.focusSection(-1); return nil })
	actReload      = appAction("reload", func(m *model) tea.Cmd { m.setStatus("reloading"); return m.loadCmd() })
	actSplitView   = appAction("split or unified", func(m *model) tea.Cmd { m.split = !m.split; m.rebuildMain(); return nil })
	actAutoAdvance = appAction("toggle auto-advance after marking", func(m *model) tea.Cmd { m.toggleAutoAdvance(); return nil })
	actSearch      = loadedApp("search", func(m *model) tea.Cmd { return m.openSearch() })
	actPickBase    = loadedApp("change the base branch", func(m *model) tea.Cmd { return m.openBase() })
	actSwitch      = appAction("switch to another change", func(m *model) tea.Cmd { return m.pickChange() })
	actCompare     = loadedApp("compare two revisions", func(m *model) tea.Cmd { return m.pickRevisions() })
	actSearchNext  = loadedApp("next match", func(m *model) tea.Cmd { m.searchStep(1); return nil })
	actSearchPrev  = loadedApp("previous match", func(m *model) tea.Cmd { m.searchStep(-1); return nil })
	actJumpBack    = loadedApp("back in the jump list", func(m *model) tea.Cmd { m.jumpBack(); return nil })
	actJumpForward = loadedApp("forward in the jump list", func(m *model) tea.Cmd { m.jumpForward(); return nil })
	actAllChanges  = loadedApp("all changes and back", func(m *model) tea.Cmd { m.toggleAllChanges(); return nil })
	actOverview    = loadedApp("overview and back", func(m *model) tea.Cmd { m.toggleView(viewOverview); return nil })
	actNextCommit  = loadedApp("next commit", func(m *model) tea.Cmd { m.stepCommit(1); return nil })
	actPrevCommit  = loadedApp("previous commit", func(m *model) tea.Cmd { m.stepCommit(-1); return nil })
	actNextUnseen  = loadedApp("next unreviewed commit", func(m *model) tea.Cmd { m.stepUnreviewed(1); return nil })
	actPrevUnseen  = loadedApp("previous unreviewed commit", func(m *model) tea.Cmd { m.stepUnreviewed(-1); return nil })
	actPalette     = loadedApp("actions here", func(m *model) tea.Cmd { return m.openPalette() })
)

// The comment box, its unsaved comment prompt, the verdict and y/n prompts, and the fold prefix:
// modes that take keys of their own while they last.
var (
	actComposeSave = &interact.Action[*model, composeSubject]{
		Title: "save",
		Run:   func(m *model, _ composeSubject) tea.Cmd { _, c := m.saveCompose(); return c },
	}
	actComposeEditor = &interact.Action[*model, composeSubject]{
		Title: "open $EDITOR",
		Run:   func(m *model, _ composeSubject) tea.Cmd { return editorCmd(m.compose.area.Value()) },
	}
	actComposeClose = &interact.Action[*model, composeSubject]{
		Title: "close, asking if unsaved",
		Run:   func(m *model, _ composeSubject) tea.Cmd { m.closeOrAsk(); return nil },
	}
	actClosingDiscard = &interact.Action[*model, composeSubject]{
		Title: "discard it",
		Run: func(m *model, _ composeSubject) tea.Cmd {
			m.closeComposer()
			m.setStatus("comment discarded")
			return nil
		},
	}
	actClosingKeep = &interact.Action[*model, composeSubject]{
		Title: "keep editing",
		Run:   do[composeSubject](func(m *model) { m.compose.closing = false }),
	}

	actConfirmYes = &interact.Action[*model, confirmSubject]{
		Title: "yes",
		Run: func(m *model, _ confirmSubject) tea.Cmd {
			run := m.confirm.run
			m.endConfirm()
			return run
		},
	}
	actConfirmNo = &interact.Action[*model, confirmSubject]{
		Title: "no",
		Run: func(m *model, _ confirmSubject) tea.Cmd {
			no := m.confirm.no
			m.endConfirm()
			if no == nil {
				return nil
			}
			return no
		},
	}
	actConfirmLater = &interact.Action[*model, confirmSubject]{
		Title:   "later",
		Help:    "answer later",
		Applies: func(m *model, _ confirmSubject) bool { return m.confirm.no != nil },
		Run:     do[confirmSubject](func(m *model) { m.endConfirm() }),
	}

	actVerdictApprove        = verdictAction("approve", store.VerdictApprove, false)
	actVerdictRequestChanges = verdictAction("request changes", store.VerdictRequestChanges, false)
	actVerdictComment        = verdictAction("comment", store.VerdictComment, false)
	actVerdictApproveNow     = verdictAction("approve, no summary", store.VerdictApprove, true)
	actVerdictRequestNow     = verdictAction("request changes, no summary", store.VerdictRequestChanges, true)
	actVerdictCommentNow     = verdictAction("comment, no summary", store.VerdictComment, true)
	actVerdictCancel         = &interact.Action[*model, verdictSubject]{
		Title: "cancel",
		Run:   do[verdictSubject](func(m *model) { m.mode = modeNormal }),
	}

	actFoldOpen     = paneAction("open the fold", func(m *model) tea.Cmd { m.fold(foldOpen); return nil })
	actFoldClose    = paneAction("close the fold", func(m *model) tea.Cmd { m.fold(foldClose); return nil })
	actFoldToggle   = paneAction("toggle the fold", func(m *model) tea.Cmd { m.fold(foldToggle); return nil })
	actFoldOpenAll  = paneAction("open every fold", func(m *model) tea.Cmd { m.foldAll(true); return nil })
	actFoldCloseAll = paneAction("close every fold", func(m *model) tea.Cmd { m.foldAll(false); return nil })
	actFoldOpenIn   = paneAction("open the fold and all under it", func(m *model) tea.Cmd { m.foldIn(true); return nil })
	actFoldCloseIn  = paneAction("close the fold and all under it", func(m *model) tea.Cmd { m.foldIn(false); return nil })
)

// verdictAction submits the draft review with verdict: at once, without a summary, or after asking
// for one.
func verdictAction(title string, verdict store.Verdict, now bool) *interact.Action[*model, verdictSubject] {
	return &interact.Action[*model, verdictSubject]{
		Title: title,
		Run:   func(m *model, _ verdictSubject) tea.Cmd { return m.submitWith(verdict, now) },
	}
}

// Kinds, and the keys bound on them. Kinds and places are made in init: actions run code that
// reads them, so making them in their declarations would be an initialization cycle.
var (
	overviewKind       *interact.Kind[*model, overviewSubject]
	diffKind           *interact.Kind[*model, diffSubject]
	interdiffKind      *interact.Kind[*model, interdiffSubject]
	commitKind         *interact.Kind[*model, commitSubject]
	changeKind         *interact.Kind[*model, changeSubject]
	fileKind           *interact.Kind[*model, fileSubject]
	lineKind           *interact.Kind[*model, rowSubject]
	messageLineKind    *interact.Kind[*model, rowSubject]
	hunkKind           *interact.Kind[*model, rowSubject]
	foldKind           *interact.Kind[*model, rowSubject]
	diffThreadKind     *interact.Kind[*model, threadSubject]
	overviewThreadKind *interact.Kind[*model, threadSubject]
	summaryKind        *interact.Kind[*model, threadSubject]
	reviewKind         *interact.Kind[*model, reviewSubject]
	sidebarPaneKind    *interact.Kind[*model, paneSubject]
	diffPaneKind       *interact.Kind[*model, paneSubject]
	overviewPaneKind   *interact.Kind[*model, paneSubject]
	appKind            *interact.Kind[*model, appSubject]
	composeBoxKind     *interact.Kind[*model, composeSubject]
	closingKind        *interact.Kind[*model, composeSubject]
	verdictKind        *interact.Kind[*model, verdictSubject]
	confirmKind        *interact.Kind[*model, confirmSubject]
	foldPrefixKind     *interact.Kind[*model, paneSubject]
)

func init() {
	overviewKind = interact.NewKind(func(overviewSubject) string { return "Overview" }, actFilterThreads)
	diffKind = interact.NewKind[*model, diffSubject](func(diffSubject) string { return "All changes" })
	interdiffKind = interact.NewKind[*model, interdiffSubject](func(interdiffSubject) string { return "Changes since" })
	commitKind = interact.NewKind(func(s commitSubject) string { return "Commit " + shortSHA(s.view.commit.SHA) },
		actMarkCommitReviewed, actCommentOnCommit)
	changeKind = interact.NewKind(func(changeSubject) string { return "Change" },
		actCommentOnChange, actSubmit, actDiscardDraft, actOpenNewWork)

	fileKind = interact.NewKind(fileLabel, actMarkFileReviewed, actCommentOnFile, actOpenFileInEditor)

	lineKind = interact.NewKind(func(rowSubject) string { return "Line" }, actCommentOnLine, actSelectLine, actSearchText, actOpenRowInEditor,
		actCollapseGap)
	messageLineKind = interact.NewKind(func(rowSubject) string { return "Message line" }, actCommentOnLine, actSelectLine, actSearchText)
	hunkKind = interact.NewKind(func(rowSubject) string { return "Hunk" }, actSelectLine, actOpenRowInEditor)
	foldKind = interact.NewKind(func(rowSubject) string { return "Unchanged lines" },
		actExpandFold, actExpandUp, actExpandDown, actExpandAll, actCollapseGap, actOpenRowInEditor)

	diffThreadKind = interact.NewKind(threadLabel, threadActions()...)
	overviewThreadKind = interact.NewKind(threadLabel, append(threadActions(), actOpenThreadInDiff, actOpenThreadInCommit)...)
	summaryKind = interact.NewKind(func(threadSubject) string { return "Summary" }, threadActions()...)
	reviewKind = interact.NewKind(func(s reviewSubject) string { return "Review #" + strconv.Itoa(s.review.Seq) },
		actDeleteReview)

	sidebarPaneKind = interact.NewKind(func(paneSubject) string { return "Sidebar" }, sidebarMoves.actions()...)
	diffPaneKind = interact.NewKind(func(paneSubject) string { return "Diff" }, append(diffMovesOf.actions(),
		actMoveLeft, actMoveRight, actNextFile, actPrevFile, actNextHunk, actPrevHunk, actNextChange, actPrevChange,
		actNextThreadInDiff, actPrevThreadInDiff, actNextUnresolved, actPrevUnresolved, actSelectLines, actFoldPrefix)...)
	overviewPaneKind = interact.NewKind(func(paneSubject) string { return "Overview" }, append(readerMovesOf.actions(),
		actToSidebar, actNextThreadInOverview, actPrevThreadInOverview, actNextUnresolved, actPrevUnresolved, actFoldPrefix)...)
	appKind = interact.NewKind(func(appSubject) string { return "gr" }, append([]*interact.Action[*model, appSubject]{
		actQuit, actShowHelp, actCancel, actNextPane, actPrevPane, actNextList, actPrevList, actReload, actSplitView,
		actAutoAdvance, actSearch, actPickBase, actCompare, actSwitch, actSearchNext, actSearchPrev, actJumpBack, actJumpForward,
		actAllChanges, actOverview, actNextCommit, actPrevCommit, actNextUnseen, actPrevUnseen, actPalette,
		actNewTab, actCloseTab, actNextTab, actPrevTab, actTabRight, actTabLeft}, actGoTab[:]...)...)

	composeBoxKind = interact.NewKind(func(composeSubject) string { return "Comment" },
		actComposeSave, actComposeEditor, actComposeClose)
	closingKind = interact.NewKind(func(composeSubject) string { return "Unsaved comment" },
		actComposeSave, actClosingDiscard, actClosingKeep)
	verdictKind = interact.NewKind(func(verdictSubject) string { return "Submit" },
		actVerdictApprove, actVerdictRequestChanges, actVerdictComment, actVerdictApproveNow, actVerdictRequestNow,
		actVerdictCommentNow, actVerdictCancel)
	confirmKind = interact.NewKind(func(confirmSubject) string { return "Confirm" }, actConfirmYes, actConfirmNo, actConfirmLater)
	foldPrefixKind = interact.NewKind(func(paneSubject) string { return "Folds" },
		actFoldOpen, actFoldClose, actFoldToggle, actFoldOpenIn, actFoldCloseIn, actFoldOpenAll, actFoldCloseAll)
}

func fileLabel(s fileSubject) string { return "File " + s.diff.Path() }

func threadLabel(s threadSubject) string { return "Thread " + s.thread.Label() }

// threadActions are what every thread does, wherever it is.
func threadActions() []*interact.Action[*model, threadSubject] {
	return []*interact.Action[*model, threadSubject]{
		actReplyToThread, actEditComment, actResolveThread, actAddressThread, actDeleteComment,
	}
}

// bindThread binds the keys of what every thread does.
func bindThread(k *interact.Kind[*model, threadSubject]) {
	k.Bind(actReplyToThread, "r", "reply", "r")
	k.Bind(actResolveThread, "x", "resolve/reopen", "x")
	k.Bind(actAddressThread, "a", "mark addressed", "a")
	k.Bind(actEditComment, "e", "edit my comment", "e")
	k.Bind(actDeleteComment, "X", "delete my comment or thread", "X")
}

func init() {
	overviewKind.Bind(actFilterThreads, "F", "filter threads", "F")
	commitKind.Bind(actCommentOnCommit, "c/C", "comment on the commit", "c", "C")
	commitKind.Bind(actMarkCommitReviewed, "m/M", "mark the commit reviewed", "m", "M")
	changeKind.Bind(actCommentOnChange, "c", "comment on the change", "c")
	changeKind.Bind(actSubmit, "S", "submit", "S")
	changeKind.Bind(actDiscardDraft, "D", "discard draft", "D")

	writable(fileKind, actCommentOnFile, "c/C", "comment on the file", "c", "C")
	writable(fileKind, actMarkFileReviewed, "m", "mark the file reviewed", "m")
	fileKind.Bind(actOpenFileInEditor, "E", "open in $EDITOR, read-only, as it is here", "E")

	writable(lineKind, actCommentOnLine, "c", "comment on the line or selection", "c")
	messageLineKind.Bind(actCommentOnLine, "c", "comment on the message's line or selection", "c")
	messageLineKind.Bind(actSelectLine, "v", "select lines", "v", "V")
	lineKind.Bind(actSelectLine, "v", "select lines", "v", "V")
	hunkKind.Bind(actSelectLine, "v", "select lines", "v", "V")
	foldKind.BindQuiet(actNoCommentOnFold, "c", "C")
	for _, k := range []*interact.Kind[*model, rowSubject]{lineKind, hunkKind, foldKind} {
		// Described on the file it is in, which takes E wherever there is no line to open at.
		k.BindQuiet(actOpenRowInEditor, "E")
	}

	bindThread(diffThreadKind)
	bindThread(overviewThreadKind)
	bindThread(summaryKind)
	overviewThreadKind.Bind(actOpenThreadInDiff, "o", "open thread in whole diff", "o")
	overviewThreadKind.Bind(actOpenThreadInCommit, "O", "open thread in its commit", "O")
	reviewKind.Bind(actDeleteReview, "D", "delete your review", "D")

	bindMoves(sidebarPaneKind, sidebarMoves)
	bindMoves(diffPaneKind, diffMovesOf)
	bindMoves(overviewPaneKind, readerMovesOf)
	for _, b := range []struct {
		action     *interact.Action[*model, paneSubject]
		help, desc string
		keys       []string
	}{
		{actMoveLeft, "h/l", "old/new side in split, or the sidebar", []string{"h", "left"}},
		{actMoveRight, "", "", []string{"l", "right"}},
		{actNextFile, "]/[", "next/prev file", []string{"]"}},
		{actPrevFile, "", "", []string{"["}},
		{actNextHunk, "J/K", "next/prev hunk", []string{"J"}},
		{actPrevHunk, "", "", []string{"K"}},
		{actNextChange, ")/(", "next/prev change", []string{")"}},
		{actPrevChange, "", "", []string{"("}},
		{actNextThreadInDiff, "t/T", "next/prev thread", []string{"t"}},
		{actPrevThreadInDiff, "", "", []string{"T"}},
		{actNextUnresolved, "i/I", "next/prev unresolved thread, across commits", []string{"i"}},
		{actPrevUnresolved, "", "", []string{"I"}},
		{actSelectLines, "v", "select lines", []string{"v", "V"}},
		{actFoldPrefix, "z", "folds, then o c a O C R M", []string{"z"}},
	} {
		diffPaneKind.Bind(b.action, b.help, b.desc, b.keys...)
	}
	overviewPaneKind.Bind(actToSidebar, "h", "sidebar", "h", "left")
	overviewPaneKind.Bind(actNextThreadInOverview, "t/T", "next/prev thread", "t")
	overviewPaneKind.Bind(actPrevThreadInOverview, "", "", "T")
	overviewPaneKind.Bind(actNextUnresolved, "i/I", "next/prev unresolved thread", "i")
	overviewPaneKind.Bind(actPrevUnresolved, "", "", "I")
	overviewPaneKind.Bind(actFoldPrefix, "z", "folds, then o c a O C R M", "z")

	for _, b := range []struct {
		action     *interact.Action[*model, appSubject]
		help, desc string
		keys       []string
	}{
		{actNextPane, "tab/shift+tab", "next/previous pane", []string{"tab"}},
		{actPrevPane, "", "", []string{"shift+tab"}},
		{actNextList, "ctrl+j/k", "files/changes list", []string{"ctrl+j"}},
		{actPrevList, "", "", []string{"ctrl+k"}},
		{actOverview, "H", "overview and back", []string{"H"}},
		{actAllChanges, "A", "all changes and back", []string{"A"}},
		{actNextCommit, "}/{", "next/prev commit", []string{"}"}},
		{actPrevCommit, "", "", []string{"{"}},
		{actNextUnseen, ">/<", "next/prev unreviewed", []string{">"}},
		{actPrevUnseen, "", "", []string{"<"}},
		{actJumpBack, "ctrl+o/i", "back/forward in jumps", []string{"ctrl+o"}},
		// Terminals without enhanced keyboard reporting send ctrl+i as tab, which switches panes.
		{actJumpForward, "", "", []string{"ctrl+i"}},
		{actSearch, "/", "search", []string{"/"}},
		{actSearchNext, "n/N", "next/prev match", []string{"n"}},
		{actSearchPrev, "", "", []string{"N"}},
		{actCancel, "esc", "clear search, selection", []string{"esc"}},
		{actAutoAdvance, "+", "auto-advance after marking", []string{"+"}},
		{actSplitView, "s", "split/unified", []string{"s"}},
		{actPickBase, "B", "change the base branch", []string{"B"}},
		{actCompare, "p", "pick two revisions to compare", []string{"p"}},
		{actSwitch, "w", "switch to another change or branch", []string{"w"}},
		{actReload, "R", "reload", []string{"R", "ctrl+r"}},
		{actPalette, "ctrl+p", "actions here", []string{"ctrl+p"}},
		{actNewTab, "ctrl+t", "open a change in a new tab", []string{"ctrl+t"}},
		{actCloseTab, "ctrl+w", "close the tab", []string{"ctrl+w"}},
		{actNextTab, "ctrl+pgdn/up", "next/prev tab", []string{"ctrl+pgdown"}},
		{actPrevTab, "", "", []string{"ctrl+pgup"}},
		{actTabRight, "ctrl+shift+pgdn/up", "move the tab right/left", []string{"ctrl+shift+pgdown"}},
		{actTabLeft, "", "", []string{"ctrl+shift+pgup"}},
		{actShowHelp, "?", "help", []string{"?"}},
		{actQuit, "q", "quit", []string{"q", "ctrl+c"}},
	} {
		appKind.Bind(b.action, b.help, b.desc, b.keys...)
	}
	for i, a := range actGoTab {
		help, desc := "", ""
		if i == 0 {
			help, desc = "alt+1..9", "go to a tab"
		}
		appKind.Bind(a, help, desc, "alt+"+strconv.Itoa(i+1))
	}

	composeBoxKind.Bind(actComposeSave, "ctrl+s", "save", "ctrl+s")
	composeBoxKind.Bind(actComposeEditor, "ctrl+e", "open $EDITOR", "ctrl+e")
	composeBoxKind.Bind(actComposeClose, "esc", "close, asking if unsaved", "esc")
	closingKind.Bind(actComposeSave, "s", "save it", "s")
	closingKind.Bind(actClosingDiscard, "d", "discard it", "d")
	closingKind.Bind(actClosingKeep, "esc", "keep editing", "esc")

	verdictKind.Bind(actVerdictApprove, "a", "approve", "a")
	verdictKind.Bind(actVerdictRequestChanges, "r", "request changes", "r")
	verdictKind.Bind(actVerdictComment, "c", "comment", "c")
	verdictKind.Bind(actVerdictApproveNow, "A R C", "skip the summary", "A")
	verdictKind.Bind(actVerdictRequestNow, "", "", "R")
	verdictKind.Bind(actVerdictCommentNow, "", "", "C")
	verdictKind.Bind(actVerdictCancel, "esc", "cancel", "esc")
	confirmKind.Bind(actConfirmYes, "y", "yes", "y")
	confirmKind.Bind(actConfirmNo, "n", "no, as any other key", "n")
	confirmKind.Bind(actConfirmLater, "esc", "answer later", "esc")

	foldPrefixKind.Bind(actFoldOpen, "zo", "open the fold", "o")
	foldPrefixKind.Bind(actFoldClose, "zc", "close the fold", "c")
	foldPrefixKind.Bind(actFoldToggle, "za", "toggle the fold", "a")
	foldPrefixKind.Bind(actFoldOpenIn, "zO", "open all under it", "O")
	foldPrefixKind.Bind(actFoldCloseIn, "zC", "close all under it", "C")
	foldPrefixKind.Bind(actFoldOpenAll, "zR", "open every fold", "R")
	foldPrefixKind.Bind(actFoldCloseAll, "zM", "close every fold", "M")
}

// What each subject carries: the overview the change it is the page of, a row or a thread its file,
// a file or a thread its commit when it has one, and a review its summary and the overview. So a
// key a subject does not take reaches what it carries. The change is reached from everywhere else
// as the chain's, after the pane.
func init() {
	interact.Via(overviewKind, func(overviewSubject) (changeSubject, bool) { return changeSubject{}, true }, changeKind)
	// A file of a commit carries its commit; one of the whole diff or the interdiff has none.
	interact.Via(fileKind, func(s fileSubject) (commitSubject, bool) {
		return commitSubject{s.inView}, s.view.kind == viewCommit
	}, commitKind)
	for _, k := range []*interact.Kind[*model, rowSubject]{lineKind, hunkKind, foldKind} {
		interact.Via(k, func(s rowSubject) (fileSubject, bool) { return s.file, s.hasFile }, fileKind)
	}
	interact.Via(diffThreadKind, func(s threadSubject) (fileSubject, bool) { return s.file, s.hasFile && !isMessage(s.file.diff) }, fileKind)
	// The message's lines, and its threads, are the commit's: it is no file of its own.
	interact.Via(messageLineKind, func(s rowSubject) (commitSubject, bool) { return commitSubject{s.inView}, true }, commitKind)
	interact.Via(diffThreadKind, func(s threadSubject) (commitSubject, bool) {
		return commitSubject{s.inView}, s.hasFile && isMessage(s.file.diff)
	}, commitKind)
	// A thread on a commit as a whole carries the commit.
	interact.Via(diffThreadKind, func(s threadSubject) (commitSubject, bool) {
		return commitSubject{s.inView}, !s.hasFile && s.view.kind == viewCommit
	}, commitKind)
	interact.Via(overviewThreadKind, func(s threadSubject) (overviewSubject, bool) { return overviewSubject{s.inView}, true }, overviewKind)
	interact.Via(reviewKind, func(s reviewSubject) (threadSubject, bool) {
		if s.summary == nil {
			return threadSubject{}, false
		}
		return threadSubject{inView: s.inView, index: s.index, thread: *s.summary}, true
	}, summaryKind)
	interact.Via(reviewKind, func(s reviewSubject) (overviewSubject, bool) { return overviewSubject{s.inView}, true }, overviewKind)
}

// Places, and the keys bound on them: what a subject does where it is.
var (
	sidebarOverview  *interact.Place[*model, overviewSubject]
	sidebarDiff      *interact.Place[*model, diffSubject]
	sidebarInterdiff *interact.Place[*model, interdiffSubject]
	sidebarCommits   *interact.Place[*model, commitSubject]
	sidebarFileList  *interact.Place[*model, fileSubject]
	commitHeader     *interact.Place[*model, commitSubject]
	fileHeaderList   *interact.Place[*model, fileSubject]
	lines            *interact.Place[*model, rowSubject]
	messageLines     *interact.Place[*model, rowSubject]
	hunks            *interact.Place[*model, rowSubject]
	folds            *interact.Place[*model, rowSubject]
	diffThreads      *interact.Place[*model, threadSubject]
	overviewThreads  *interact.Place[*model, threadSubject]
	reviewBars       *interact.Place[*model, reviewSubject]
	overviewRows     *interact.Place[*model, overviewSubject]
	theChange        *interact.Place[*model, changeSubject]
	theSidebarPane   *interact.Place[*model, paneSubject]
	theDiffPane      *interact.Place[*model, paneSubject]
	theOverviewPane  *interact.Place[*model, paneSubject]
	theApp           *interact.Place[*model, appSubject]
	theComposer      *interact.Place[*model, composeSubject]
	theClosing       *interact.Place[*model, composeSubject]
	theVerdict       *interact.Place[*model, verdictSubject]
	theConfirm       *interact.Place[*model, confirmSubject]
	theFoldPrefix    *interact.Place[*model, paneSubject]
)

func init() {
	sidebarOverview = sidebarView(overviewKind, actShowOverview, actOpenOverview)
	sidebarDiff = sidebarView(diffKind, actShowDiff, actOpenDiff)
	sidebarInterdiff = sidebarView(interdiffKind, actShowInterdiff, actOpenInterdiff)
	sidebarCommits = sidebarView(commitKind, actShowCommit, actOpenCommit)

	sidebarFileList = sidebarFiles(fileKind)

	commitHeader = interact.List(commitKind)
	fileHeaderList = fileHeaders(fileKind)
	lines = interact.List(lineKind)
	messageLines = interact.List(messageLineKind)
	hunks = interact.List(hunkKind)
	folds = interact.List(foldKind)
	diffThreads = interact.List(diffThreadKind)
	overviewThreads = interact.List(overviewThreadKind)
	reviewBars = interact.List(reviewKind)
	overviewRows = interact.List(overviewKind)

	theChange = interact.List(changeKind)
	theSidebarPane = interact.List(sidebarPaneKind)
	theDiffPane = interact.List(diffPaneKind)
	theOverviewPane = interact.List(overviewPaneKind)
	theApp = interact.List(appKind)
	theComposer = interact.List(composeBoxKind)
	theClosing = interact.List(closingKind)
	theVerdict = interact.List(verdictKind)
	theConfirm = interact.List(confirmKind)
	theFoldPrefix = interact.List(foldPrefixKind)
}

// sidebarView is a place of the sidebar's view list: the cursor landing on an entry shows it, and
// choosing one opens it.
func sidebarView[S any](k *interact.Kind[*model, S], show, open *interact.Action[*model, S]) *interact.Place[*model, S] {
	p := interact.List(k, interact.OnCursor(show))
	p.Bind(open, "enter/l", "open", "enter", "l", "right")
	return p
}

// sidebarFiles is a place of the sidebar's file list.
func sidebarFiles(k *interact.Kind[*model, fileSubject]) *interact.Place[*model, fileSubject] {
	p := interact.List(k, interact.OnCursor(actShowFile))
	p.Bind(actOpenFile, "enter/l", "open", "enter", "l", "right")
	p.Bind(actScrollNamesBack, "shift+left/right", "scroll long names", "shift+left")
	p.Bind(actScrollNamesOn, "", "", "shift+right")
	return p
}

// fileHeaders is a place of the files' headers in a diff, which enter folds.
func fileHeaders(k *interact.Kind[*model, fileSubject]) *interact.Place[*model, fileSubject] {
	p := interact.List(k)
	p.Bind(actFoldFile, "enter", "fold or expand", "enter")
	return p
}

func init() {
	folds.Bind(actExpandFold, "enter", "expand", "enter")
	diffThreads.Bind(actToggleThread, "enter", "expand/collapse thread", "enter")
	overviewThreads.Bind(actToggleThread, "enter", "expand/collapse thread", "enter")
	reviewBars.Bind(actToggleReview, "enter", "expand/collapse the summary", "enter")
}

// commitMentions is a commit named where something else is, as the sha in a thread's location.
// Drawing marks with it, and actions draw, so it is made in init to break the cycle.
var commitMentions *interact.Mentions[*model, commitSubject]

func init() {
	commitMentions = interact.Mention(commitKind, (*model).commitNamed, interact.Offers(actGoToCommit))
}

// Targets: the subjects on screen, each in its place.

// viewTarget is view i, in place p: the sidebar's entry, or the view itself in the main pane.
func (m *model) viewTarget(i int, sidebar bool) interact.Target[*model] {
	v := m.inView(i)
	switch v.view.kind {
	case viewOverview:
		if sidebar {
			return sidebarOverview.Target(overviewSubject{v})
		}
		return overviewRows.Target(overviewSubject{v})
	case viewCommit:
		if sidebar {
			return sidebarCommits.Target(commitSubject{v})
		}
		return commitHeader.Target(commitSubject{v})
	case viewInterdiff:
		return sidebarInterdiff.Target(interdiffSubject{v})
	}
	return sidebarDiff.Target(diffSubject{v})
}

// fileTarget is file f, as an entry of the sidebar's file list or as its header in the main pane.
func (m *model) fileTarget(f fileSubject, sidebar bool) interact.Target[*model] {
	if sidebar {
		return sidebarFileList.Target(f)
	}
	return fileHeaderList.Target(f)
}

// cursorTarget is the subject under the cursor where focus is, in its place.
func (m *model) cursorTarget() (interact.Target[*model], bool) {
	if m.data == nil {
		return interact.Target[*model]{}, false
	}
	if m.focus == focusFiles {
		return m.sidebarEntry(focusFiles, m.filesNav.pos)
	}
	if m.focus == focusViews {
		return m.sidebarEntry(focusViews, m.viewsNav.pos)
	}
	return m.mainRowTarget(m.main.cursor, m.main.side)
}

// mainRowTarget is row i of the main pane, on side in split mode, in its place: the cursor's row
// for keys, or the row a right click lands on, which leaves the cursor where it is.
func (m *model) mainRowTarget(i int, side anchor.Side) (interact.Target[*model], bool) {
	shown := m.inView(m.current)
	if i < 0 || i >= len(m.main.rows) {
		return m.viewTarget(m.current, false), true
	}
	r := m.main.rows[i]
	file, hasFile := m.fileOf(shown, r.file)
	row := rowSubject{inView: shown, index: i, side: side, file: file, hasFile: hasFile, row: r}
	switch {
	case r.kind == rowCommit:
		return commitHeader.Target(commitSubject{shown}), true
	case r.kind == rowFile && hasFile:
		return m.fileTarget(file, false), true
	case r.kind == rowThread && r.thread != nil:
		t := threadSubject{inView: shown, index: i, file: file, hasFile: hasFile, thread: *r.thread}
		if shown.view.kind == viewOverview {
			return overviewThreads.Target(t), true
		}
		return diffThreads.Target(t), true
	case r.kind == rowReview && r.review != nil:
		return reviewBars.Target(reviewSubject{inView: shown, index: i, review: *r.review, summary: r.thread}), true
	}
	switch r.kind {
	case rowGap:
		return folds.Target(row), true
	case rowHunk:
		return hunks.Target(row), true
	case rowLine:
		if hasFile && isMessage(file.diff) {
			return messageLines.Target(row), true
		}
		return lines.Target(row), true
	}
	return m.viewTarget(m.current, false), true
}

// sidebarEntry is entry i of a sidebar list, in its place.
func (m *model) sidebarEntry(list focus, i int) (interact.Target[*model], bool) {
	l := &m.viewsNav
	if list == focusFiles {
		l = &m.filesNav
	}
	if i < 0 || i >= len(l.items) {
		return interact.Target[*model]{}, false
	}
	it := l.items[i]
	if list == focusFiles {
		f, ok := m.fileOf(m.inView(it.view), it.file)
		return m.fileTarget(f, true), ok
	}
	return m.viewTarget(it.view, true), true
}

// paneTarget is the pane t is in: the sidebar, or the main pane's diff or overview.
func (m *model) paneTarget(sidebar bool) interact.Target[*model] {
	switch {
	case sidebar:
		return theSidebarPane.Target(paneSubject{})
	case m.data != nil && m.views[m.current].kind == viewOverview:
		return theOverviewPane.Target(paneSubject{})
	}
	return theDiffPane.Target(paneSubject{})
}

// chain is what a key reaches from t, the most specific first: t, with what it carries, then its
// pane, the change once it is loaded, and gr itself. The change is about everything on screen, so
// every subject reaches it, though only the overview carries it into its menu.
func (m *model) chain(t interact.Target[*model], has bool, sidebar bool) []interact.Target[*model] {
	var out []interact.Target[*model]
	if has {
		out = append(out, t)
	}
	out = append(out, m.paneTarget(sidebar))
	if m.data != nil {
		out = append(out, theChange.Target(changeSubject{}))
	}
	return append(out, theApp.Target(appSubject{}))
}

// cursorChain is what a key reaches where focus is.
func (m *model) cursorChain() []interact.Target[*model] {
	t, ok := m.cursorTarget()
	return m.chain(t, ok, m.focus != focusMain)
}

// chainOf is what a key would reach from t, for a menu opened on it: none for a mention, which no
// key reaches.
func (m *model) chainOf(t interact.Target[*model]) []interact.Target[*model] {
	if t.Mention() {
		return nil
	}
	sidebar := false
	for _, p := range []interface {
		Holds(interact.Target[*model]) bool
	}{sidebarOverview, sidebarDiff, sidebarInterdiff, sidebarCommits, sidebarFileList} {
		sidebar = sidebar || p.Holds(t)
	}
	return m.chain(t, true, sidebar)
}

// dispatch runs what msg reaches where focus is, and reports whether anything ran.
func (m *model) dispatch(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return m.runOn(msg, m.cursorChain()...)
}

// runOn runs what msg reaches on chain, and reports whether anything ran. Modes and prefixes look
// up their keys this way, each on its own target.
func (m *model) runOn(msg tea.KeyPressMsg, chain ...interact.Target[*model]) (tea.Cmd, bool) {
	a, ok := interact.Lookup(m, msg, chain...)
	if !ok {
		return nil, false
	}
	return a.Run(m), true
}

// pressed reports whether msg runs a on t.
func pressed[S any](m *model, msg tea.KeyPressMsg, a *interact.Action[*model, S], t interact.Target[*model]) bool {
	b, ok := interact.Lookup(m, msg, t)
	return ok && b.Is(a)
}

// discardDraft asks to discard your draft review and every comment in it.
func (m *model) discardDraft() tea.Cmd {
	svc, ctx, change := m.svc, m.ctx, m.data.change
	m.ask("Discard your draft review and all its comments?", func() tea.Msg {
		return opMsg{status: "draft review discarded", err: svc.Discard(ctx, change)}
	})
	return nil
}

// commitNamed is the commit named by sha, for a mention of it.
func (m *model) commitNamed(sha string) (commitSubject, bool) {
	for i, v := range m.views {
		if v.kind == viewCommit && v.commit.SHA == sha {
			return commitSubject{m.inView(i)}, true
		}
	}
	return commitSubject{}, false
}

// mentionAt is the mention drawn at x, y in the last frame, if any.
func (m *model) mentionAt(x, y int) (interact.Target[*model], bool) {
	for id, zones := range m.zones {
		for _, z := range zones {
			if z.Contains(x, y) {
				return commitMentions.At(m, id)
			}
		}
	}
	return interact.Target[*model]{}, false
}
