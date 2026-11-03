package interact_test

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/tui/interact"
)

// env records what actions ran, as the TUI's model would interact.
type env struct {
	ran  []string
	mine bool
}

type commit struct {
	sha string
}

type file struct {
	path   string
	commit commit
}

func action[S any](title string, applies func(*env, S) bool) *interact.Action[*env, S] {
	return &interact.Action[*env, S]{Title: title, Applies: applies,
		Run: func(e *env, _ S) tea.Cmd { e.ran = append(e.ran, title); return nil }}
}

func press(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

// kinds are commits and files, a file carrying its commit, each with keys of its own: a file takes
// c and m, and a commit takes c and C, m and M, so C and M on a file reach its commit.
func kinds() (*interact.Kind[*env, commit], *interact.Kind[*env, file]) {
	openCommit := action[commit]("open", nil)
	markCommit := action[commit]("mark commit reviewed", nil)
	commentOnCommit := action[commit]("comment on commit", nil)
	editCommit := action[commit]("edit", func(e *env, _ commit) bool { return e.mine })
	commits := interact.NewKind(func(c commit) string { return "Commit " + c.sha }, openCommit, markCommit, commentOnCommit, editCommit)
	commits.Bind(openCommit, "o", "open", "o")
	commits.Bind(markCommit, "m/M", "mark reviewed", "m", "M")
	commits.Bind(commentOnCommit, "c/C", "comment", "c", "C")
	commits.Bind(editCommit, "e", "edit", "e")

	markFile := action[file]("mark file reviewed", nil)
	commentOnFile := action[file]("comment on file", nil)
	files := interact.NewKind(func(f file) string { return "File " + f.path }, markFile, commentOnFile)
	files.Bind(markFile, "m", "mark reviewed", "m")
	files.Bind(commentOnFile, "c", "comment", "c")
	interact.Via(files, func(f file) (commit, bool) { return f.commit, f.commit.sha != "" }, commits)
	return commits, files
}

type groupTitles struct {
	Label   string
	Actions []string
}

func titles(gs []interact.Group[*env]) []groupTitles {
	var out []groupTitles
	for _, g := range gs {
		n := groupTitles{Label: g.Label}
		for _, a := range g.Actions {
			n.Actions = append(n.Actions, a.Title)
		}
		out = append(out, n)
	}
	return out
}

// A subject's actions come first, then those of the subjects it carries, each under its label, and
// only those that apply.
func TestGroups(t *testing.T) {
	_, files := kinds()
	p := interact.List(files)
	e := &env{}
	got := titles(p.Target(file{path: "a.go", commit: commit{sha: "abc"}}).Groups(e))
	want := []groupTitles{
		{Label: "File a.go", Actions: []string{"mark file reviewed", "comment on file"}},
		{Label: "Commit abc", Actions: []string{"open", "mark commit reviewed", "comment on commit"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %+v, want %+v", got, want)
	}
	e.mine = true
	got = titles(p.Target(file{path: "a.go"}).Groups(e))
	want = []groupTitles{{Label: "File a.go", Actions: []string{"mark file reviewed", "comment on file"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups without a commit = %+v, want %+v", got, want)
	}
}

// A key runs on the most specific subject that takes it, and passes to what it carries otherwise:
// a file takes m, its commit M and o, and nothing takes e while edit does not apply.
func TestLookup(t *testing.T) {
	_, files := kinds()
	target := interact.List(files).Target(file{path: "a.go", commit: commit{sha: "abc"}})
	e := &env{}
	for _, k := range []string{"m", "M", "o", "e"} {
		if a, ok := interact.Lookup(e, press(k), target); ok {
			a.Run(e)
		}
	}
	if want := []string{"mark file reviewed", "mark commit reviewed", "open"}; !reflect.DeepEqual(e.ran, want) {
		t.Errorf("ran %q, want %q", e.ran, want)
	}
}

// The keys that reach something on a target leave out what a subject before takes: on a file, the
// commit's c/C shows as C, and its m/M as M.
func TestKeys(t *testing.T) {
	_, files := kinds()
	target := interact.List(files).Target(file{path: "a.go", commit: commit{sha: "abc"}})
	var got [][2]string
	for _, k := range interact.Keys(&env{}, target) {
		got = append(got, [2]string{k.Help().Key, k.Action.Title})
	}
	want := [][2]string{
		{"m", "mark file reviewed"},
		{"c", "comment on file"},
		{"o", "open"},
		{"M", "mark commit reviewed"},
		{"C", "comment on commit"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %q, want %q", got, want)
	}
}

// A place's keys come before its kind's, and its actions first in its menu; the cursor landing on
// a subject runs the place's handler.
func TestPlaces(t *testing.T) {
	commits, _ := kinds()
	open := action[commit]("open in the main pane", nil)
	show := action[commit]("show", nil)
	e := &env{}
	p := interact.List(commits, interact.OnCursor(show))
	p.Bind(open, "enter/o", "open", "enter", "o")
	target := p.Target(commit{sha: "abc"})
	target.OnCursor(e)
	if a, ok := interact.Lookup(e, press("o"), target); ok {
		a.Run(e)
	}
	if want := []string{"show", "open in the main pane"}; !reflect.DeepEqual(e.ran, want) {
		t.Errorf("ran %q, want %q: the handler, then the place's o over the kind's", e.ran, want)
	}
	if got := titles(target.Groups(e))[0].Actions[0]; got != "open in the main pane" {
		t.Errorf("the place's menu starts with %q", got)
	}
	if !p.Holds(target) || interact.List(commits).Holds(target) || !target.Involves(p) || !target.Involves(commits) {
		t.Error("a target is not held by its own place alone, or does not involve its place and kind")
	}
}

func TestMentions(t *testing.T) {
	commits, _ := kinds()
	e := &env{}
	open := action[commit]("go to", nil)
	mentions := interact.Mention(commits, func(_ *env, sha string) (commit, bool) {
		return commit{sha: sha}, sha == "abc"
	}, interact.Offers(open))
	// A mention is found again from the zone it is marked with, and only from its own.
	mention, ok := mentions.At(e, mentions.Zone("abc"))
	if !ok {
		t.Fatalf("no mention at zone %q", mentions.Zone("abc"))
	}
	if !mention.Mention() || titles(mention.Groups(e))[0].Actions[0] != "go to" {
		t.Errorf("mention %+v is not one, or does not offer its place's action first", titles(mention.Groups(e)))
	}
	if _, ok := mentions.At(e, mentions.Zone("def")); ok {
		t.Error("a zone of a commit that is not there resolved")
	}
	other := interact.Mention(commits, func(_ *env, sha string) (commit, bool) { return commit{sha: sha}, true })
	if _, ok := mentions.At(e, other.Zone("abc")); ok {
		t.Error("another place's zone resolved")
	}
}

// Binding an action a kind does not list panics, so no key runs what menus never offer by
// accident; BindQuiet does so on purpose, for a key that only says why it does nothing.
func TestBindQuiet(t *testing.T) {
	refuse := action[file]("read-only", nil)
	files := interact.NewKind[*env, file](func(f file) string { return "File " + f.path })
	func() {
		defer func() {
			if recover() == nil {
				t.Error("binding an action the kind does not list did not panic")
			}
		}()
		files.Bind(refuse, "c", "comment", "c")
	}()
	files.BindQuiet(refuse, "c")
	target := interact.List(files).Target(file{path: "a.go"})
	e := &env{}
	if a, ok := interact.Lookup(e, press("c"), target); ok {
		a.Run(e)
	}
	if !reflect.DeepEqual(e.ran, []string{"read-only"}) || len(target.Groups(e)) != 0 || files.Bindings()[0].Help().Desc != "" {
		t.Errorf("quiet key ran %q, menu %+v, help %+v; want it run, unlisted, and undescribed", e.ran, target.Groups(e), files.Bindings())
	}
}
