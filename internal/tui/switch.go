package tui

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/ignore"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// openChange is what picking a change does: switch the tab to it, or open it in a new tab, with
// the service of its repository.
type openChange func(m *model, svc *review.Service, c store.Change) tea.Cmd

// pickChange opens a picker of this repository's open changes, then its branches with no change
// yet, to switch to without leaving gr. A branch picked gets a change onto the default branch.
func (m *model) pickChange() tea.Cmd {
	items, check, err := m.repoItems(m.svc, func(m *model, _ *review.Service, c store.Change) tea.Cmd { return m.switchChange(c) }, true)
	if err != nil {
		m.setError(err)
		return nil
	}
	p := newPicker("Switch to a change", items, runItem(m))
	return tea.Batch(m.openPicker(p), m.checkLanded(p, check))
}

// inNewTab opens a change in a new tab.
func inNewTab(m *model, svc *review.Service, c store.Change) tea.Cmd {
	// The new tab offers what the branch picked was copied from.
	msg := newTabMsg{svc: svc, change: c, rename: m.renamed}
	m.renamed = nil
	return tabCmd(msg)
}

// pickTab opens a picker of the changes and branches of this repository, then the open changes of
// the other repositories gr has reviewed, each under its name and path, then a way to open any
// repository by its path, to open a change in a new tab.
func (m *model) pickTab() tea.Cmd {
	items, check, err := m.repoItems(m.svc, inNewTab, true)
	if err != nil {
		m.setError(err)
		return nil
	}
	var others []review.RepoChanges
	if m.openRepo != nil {
		repos, err := m.svc.OtherRepos(m.ctx)
		if err != nil {
			m.setError(err)
			return nil
		}
		others = repos
		for _, r := range repos {
			dir := r.Repo.LastPath
			name := m.repoName(dir)
			items = append(items, repoHeading(name, dir))
			for _, c := range r.Changes {
				items = append(items, pickerItem{value: changeLabel(c), full: name + ": " + changeLabel(c), key: c.ID,
					run: func(m *model) tea.Cmd {
						svc, err := m.openRepo(m.ctx, dir)
						if err != nil {
							m.setError(fmt.Errorf("cannot open %s: %w", dir, err))
							return nil
						}
						return inNewTab(m, svc, c)
					}})
			}
		}
		items = append(items, pickerItem{value: "Elsewhere", heading: true},
			pickerItem{value: "Open a repository by its path", run: (*model).pickRepoPath})
	}
	p := newPicker("Open in a new tab", items, runItem(m))
	return tea.Batch(m.openPicker(p), m.checkLanded(p, check), m.checkLanded(p, m.otherReposLanded(others)))
}

// landedCheck finds which of a picker's changes landed since gr last looked, by id, each with
// the entry to put in its place, or nil to drop it.
type landedCheck func(ctx context.Context) (map[string]*pickerItem, error)

// landedMsg is what check found of picker p's changes.
type landedMsg struct {
	picker *picker
	landed map[string]*pickerItem
	err    error
}

// checkLanded runs check in the background, as reading git for it can take seconds in a large
// repository, while picker p shows it is checking. Closing p cancels it.
func (m *model) checkLanded(p *picker, check landedCheck) tea.Cmd {
	if check == nil {
		return nil
	}
	if p.cancel == nil {
		p.ctx, p.cancel = context.WithCancel(m.ctx)
	}
	p.checking++
	ctx := p.ctx
	return func() tea.Msg {
		landed, err := check(ctx)
		return landedMsg{picker: p, landed: landed, err: err}
	}
}

// otherReposLanded checks which of the other repositories' changes have landed, as this
// repository's own are when listed: their git has to be opened for it. A repository gr cannot
// open, as a worktree since removed, is left as listed.
func (m *model) otherReposLanded(repos []review.RepoChanges) landedCheck {
	if len(repos) == 0 {
		return nil
	}
	openRepo := m.openRepo
	return func(ctx context.Context) (map[string]*pickerItem, error) {
		landed := map[string]*pickerItem{}
		for _, r := range repos {
			svc, err := openRepo(ctx, r.Repo.LastPath)
			if err != nil {
				slog.Debug("cannot open a repository to check its changes", "dir", r.Repo.LastPath, "err", err)
				continue
			}
			changes, err := svc.MarkLanded(ctx, r.Changes)
			if err != nil {
				slog.Debug("cannot check a repository's changes", "dir", r.Repo.LastPath, "err", err)
				continue
			}
			for _, c := range changes {
				if c.State != store.ChangeOpen {
					landed[c.ID] = nil
				}
			}
		}
		return landed, nil
	}
}

// pickRepoPath asks for the path to a repository, and then which of its changes or branches to
// open in a new tab.
func (m *model) pickRepoPath() tea.Cmd {
	prompt := newPrompt("Open a repository", "path to a repository; tab completes", func(m *model, text string) tea.Cmd {
		dir, err := expandPath(text)
		if err == nil {
			var svc *review.Service
			if svc, err = m.openRepo(m.ctx, dir); err == nil {
				var items []pickerItem
				var check landedCheck
				if items, check, err = m.repoItems(svc, inNewTab, true); err == nil {
					p := newPicker("Open in a new tab", items, runItem(m))
					return tea.Batch(m.openPicker(p), m.checkLanded(p, check))
				}
			}
		}
		m.setError(fmt.Errorf("cannot open %s: %w", text, err))
		return nil
	})
	prompt.suggest = pathSuggestions
	return m.openPicker(prompt)
}

// maxSuggestions bounds how many directories a path prompt offers.
const maxSuggestions = 50

// pathSuggestions are the directories a path typed so far completes to: those in the directory
// it names whose names start with the rest, each with a trailing slash to go on into it, and noted
// when it is a git repository. Hidden ones are offered once a dot is typed. A ~ typed stays a ~.
func pathSuggestions(text string) []pickerItem {
	if text == "" {
		return nil
	}
	dirPart, prefix := text, ""
	if !strings.HasSuffix(text, "/") {
		dirPart, prefix = filepath.Dir(text)+"/", filepath.Base(text)
		if !strings.Contains(text, "/") {
			dirPart = ""
		}
	}
	dir, err := expandPath(cmp.Or(dirPart, "."))
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []pickerItem
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		if info, err := os.Stat(full); err != nil || !info.IsDir() {
			continue
		}
		it := pickerItem{value: dirPart + name + "/"}
		if _, err := os.Stat(filepath.Join(full, ".git")); err == nil {
			it.note = "repository"
		}
		out = append(out, it)
		if len(out) == maxSuggestions {
			break
		}
	}
	return out
}

// expandPath is a path typed in, with ~ for the home directory, made absolute.
func expandPath(p string) (string, error) {
	if rest, ok := strings.CutPrefix(p, "~"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, rest)
	}
	return filepath.Abs(p)
}

// repoName is what the repository at dir is called: its name, rather than a worktree's own. It
// asks git once per directory, as the tab bar draws it on every frame.
func (m *model) repoName(dir string) string {
	if name, ok := m.repoNames[dir]; ok {
		return name
	}
	name, err := git.RepoName(m.ctx, dir)
	if err != nil {
		name = filepath.Base(dir)
	}
	if m.repoNames == nil {
		m.repoNames = map[string]string{}
	}
	m.repoNames[dir] = name
	return name
}

// repoHeading heads a repository's entries with its name and path.
func repoHeading(name, dir string) pickerItem {
	return pickerItem{value: name, note: dir, heading: true}
}

// repoItems are svc's repository's entries, under a heading naming it: its open changes, then,
// with branches, its branches with no change yet, which open as a new change onto the default
// branch. Typing hides the heading, so each entry then shows its repository too. The changes are as
// gr last knew them; the check returned finds those landed since.
func (m *model) repoItems(svc *review.Service, open openChange, branches bool) ([]pickerItem, landedCheck, error) {
	dir := svc.Repo().LastPath
	name := m.repoName(dir)
	changes, err := svc.ListChanges(m.ctx, store.ChangeOpen)
	if err != nil {
		return nil, nil, err
	}
	items := []pickerItem{repoHeading(name, dir)}
	reviewed := map[string]bool{}
	for _, c := range changes {
		note := ""
		if c.ID == m.changeID {
			note = "current"
		}
		if c.Kind == store.KindBranch {
			reviewed[c.Branch] = true
		}
		items = append(items, pickerItem{value: changeLabel(c), full: name + ": " + changeLabel(c), note: note, key: c.ID,
			run: func(m *model) tea.Cmd { return open(m, svc, c) }})
	}
	branchItem := func(b string) pickerItem {
		return pickerItem{value: b, full: name + ": " + b, note: "branch, new change",
			run: func(m *model) tea.Cmd { return m.reviewBranch(svc, b, open) }}
	}
	var def string
	// held are the branches open changes have, which show as those changes.
	held := map[string]bool{}
	if branches {
		all, err := svc.Git().Branches(m.ctx)
		if err != nil {
			return nil, nil, err
		}
		def, _ = svc.Git().DefaultBranch(m.ctx)
		for _, b := range all {
			if b == def {
				continue
			}
			if reviewed[b] {
				held[b] = true
				continue
			}
			items = append(items, branchItem(b))
		}
	}
	// Changes that landed since gr last looked are not open any more; the branch of one, if left,
	// is offered as any branch without an open change is.
	check := func(ctx context.Context) (map[string]*pickerItem, error) {
		changes, err := svc.MarkLanded(ctx, changes)
		if err != nil {
			return nil, err
		}
		landed := map[string]*pickerItem{}
		for _, c := range changes {
			if c.State == store.ChangeOpen {
				continue
			}
			landed[c.ID] = nil
			if c.Kind == store.KindBranch && held[c.Branch] {
				landed[c.ID] = new(branchItem(c.Branch))
			}
		}
		return landed, nil
	}
	return items, check, nil
}

// changeLabel names a change in the picker: its number, and its branch and base or its title.
func changeLabel(c store.Change) string {
	if c.Kind == store.KindBranch {
		return fmt.Sprintf("#%d %s onto %s", c.Seq, c.Branch, c.BaseBranch)
	}
	return fmt.Sprintf("#%d %s", c.Seq, c.Title)
}

// reviewBranch finds or makes the change for branch b of svc's repository, onto the default
// branch, and opens it.
func (m *model) reviewBranch(svc *review.Service, b string, open openChange) tea.Cmd {
	// A branch renamed from one with a change takes that change along, as the CLI does.
	r, ok, err := svc.FollowRename(m.ctx, b)
	if err != nil {
		m.setError(err)
		return nil
	}
	t, err := svc.ParseTarget(m.ctx, b, "")
	if err != nil {
		m.setError(err)
		return nil
	}
	c, _, err := svc.EnsureChange(m.ctx, t)
	if err != nil {
		m.setError(err)
		return nil
	}
	if ok && r.Kept {
		m.renamed = &r
	}
	cmd := open(m, svc, c)
	if m.renamed != nil {
		// Opened here, not in a new tab, which took it.
		m.offerRename(*m.renamed)
		m.renamed = nil
	}
	if ok && !r.Kept {
		// After opening, which says what it opened.
		m.setStatus("change #%d follows %s, renamed from %s", r.Change.Seq, b, r.From)
	}
	return cmd
}

// movedMsg is a change moved onto the branch of the one shown, to show in its place.
type movedMsg struct {
	change store.Change
	err    error
}

// offerRename asks whether r's change, of the branch the one shown was copied from, should follow
// this branch instead, in place of the new change shown, which goes.
func (m *model) offerRename(r review.Rename) {
	svc, ctx := m.svc, m.ctx
	m.ask(fmt.Sprintf("%s was copied from %s. Move change #%d onto %s, in place of this new change?", r.To, r.From, r.Change.Seq, r.To),
		func() tea.Msg {
			c, _, err := svc.SetBranch(ctx, r.Change, r.To, store.ByCopy)
			return movedMsg{change: c, err: err}
		})
}

// switchChange shows change c in place of the one shown: everything about the old change is
// dropped, and how gr looks, its theme, panes, and split, is kept.
func (m *model) switchChange(c store.Change) tea.Cmd {
	if c.ID == m.changeID {
		return nil
	}
	m.savePlace()
	m.changeID = c.ID
	m.data, m.views, m.current = nil, nil, 0
	m.viewsNav, m.filesNav = navList{}, navList{}
	m.search, m.searchPos, m.searchHits = "", 0, 0
	m.pendingLoads, m.pendingThread, m.pendingFile, m.comparing = nil, nil, "", nil
	m.jumps, m.jumpAt, m.returns = nil, 0, map[viewKind]jumpPoint{}
	m.threadFolds, m.openGaps, m.expanded = map[string]bool{}, map[string]gapShown{}, map[string]bool{}
	m.showIgnored = false
	m.hidden, m.fileFilter, m.showHidden = map[string]bool{}, ignore.Filter{}, false
	m.picking, m.picked = false, map[string]bool{}
	m.contents, m.segments = map[string][]string{}, map[int][]int{}
	scrolloff, height := m.main.scrolloff, m.main.height
	m.main = pane{anchor: -1, side: m.main.side, scrolloff: scrolloff, height: height}
	m.pendingPlace, m.savedPlace = nil, nil
	if p, ok := m.readPlace(c); ok {
		m.savedPlace = &p
	}
	m.setStatus("switched to %s", changeLabel(c))
	return m.loadCmd()
}
