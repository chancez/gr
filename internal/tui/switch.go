package tui

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// openChange is what picking a change does: switch the tab to it, or open it in a new tab, with
// the service of its repository.
type openChange func(m *model, svc *review.Service, c store.Change) tea.Cmd

// pickChange opens a picker of this repository's open changes, then its branches with no change
// yet, to switch to without leaving gr. A branch picked gets a change onto the default branch.
func (m *model) pickChange() tea.Cmd {
	items, err := m.repoItems(m.svc, func(m *model, _ *review.Service, c store.Change) tea.Cmd { return m.switchChange(c) }, true)
	if err != nil {
		m.setError(err)
		return nil
	}
	return m.openPicker(newPicker("Switch to a change", items, runItem(m)))
}

// inNewTab opens a change in a new tab.
func inNewTab(_ *model, svc *review.Service, c store.Change) tea.Cmd {
	return tabCmd(newTabMsg{svc: svc, change: c})
}

// pickTab opens a picker of the changes and branches of this repository, then the open changes of
// the other repositories gr has reviewed, each under its name and path, then a way to open any
// repository by its path, to open a change in a new tab.
func (m *model) pickTab() tea.Cmd {
	items, err := m.repoItems(m.svc, inNewTab, true)
	if err != nil {
		m.setError(err)
		return nil
	}
	if m.openRepo != nil {
		repos, err := m.svc.OtherRepos(m.ctx)
		if err != nil {
			m.setError(err)
			return nil
		}
		for _, r := range repos {
			dir := r.Repo.LastPath
			name := m.repoName(dir)
			items = append(items, repoHeading(name, dir))
			for _, c := range r.Changes {
				items = append(items, pickerItem{value: changeLabel(c), full: name + ": " + changeLabel(c),
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
	return m.openPicker(newPicker("Open in a new tab", items, runItem(m)))
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
				if items, err = m.repoItems(svc, inNewTab, true); err == nil {
					return m.openPicker(newPicker("Open in a new tab", items, runItem(m)))
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
// branch. Typing hides the heading, so each entry then shows its repository too.
func (m *model) repoItems(svc *review.Service, open openChange, branches bool) ([]pickerItem, error) {
	dir := svc.Repo().LastPath
	name := m.repoName(dir)
	changes, err := svc.ListChanges(m.ctx, store.ChangeOpen)
	if err != nil {
		return nil, err
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
		items = append(items, pickerItem{value: changeLabel(c), full: name + ": " + changeLabel(c), note: note,
			run: func(m *model) tea.Cmd { return open(m, svc, c) }})
	}
	if !branches {
		return items, nil
	}
	all, err := svc.Git().Branches(m.ctx)
	if err != nil {
		return nil, err
	}
	def, _ := svc.Git().DefaultBranch(m.ctx)
	for _, b := range all {
		if reviewed[b] || b == def {
			continue
		}
		items = append(items, pickerItem{value: b, full: name + ": " + b, note: "branch, new change",
			run: func(m *model) tea.Cmd { return m.reviewBranch(svc, b, open) }})
	}
	return items, nil
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
	cmd := open(m, svc, c)
	if ok && !r.Kept {
		// After opening, which says what it opened.
		m.setStatus("change #%d follows %s, renamed from %s", r.Change.Seq, b, r.From)
	}
	return cmd
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
	m.threadFolds, m.openGaps, m.expanded = map[string]bool{}, map[string]bool{}, map[string]bool{}
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
