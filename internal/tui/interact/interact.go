// Package interact is how the TUI is acted on, as look is how it is drawn: the registry of ADR 10,
// with kinds of subject and the actions on them, places that hold subjects, the targets a place
// makes of its subjects, and the keys bound on kinds and places.
//
// Everything is typed by M, the model actions run against, as in Elm's Update, and S, the subject.
// An Action[M, S] can only be listed on a Kind[M, S], bound on a Kind or Place of S, and run on an
// S, so an action on files cannot be put on a commit, and a place of commits cannot be handed a
// file. The types are erased only inside a Target, which lists a subject's actions and keys
// together.
//
// Actions, kinds, and places have no names. Each is used by its address, so two can never be
// confused, and nothing is looked up by a string.
package interact

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Action is something done to a subject of type S. It is used by its address: kinds and places
// hold an *Action, and keys are bound to one.
type Action[M, S any] struct {
	// Title is the action in menus and the hint bar, such as "mark reviewed".
	Title string
	// Help describes the action where the title is not enough, as in a hint next to another of
	// the same title. Empty uses Title.
	Help string
	// Applies reports whether the action applies to s now. Nil always applies.
	Applies func(m M, s S) bool
	// Run does it.
	Run func(m M, s S) tea.Cmd
}

func (a *Action[M, S]) applies(m M, s S) bool {
	return a.Applies == nil || a.Applies(m, s)
}

// binding is keys bound to an action of S.
type binding[M, S any] struct {
	key.Binding
	action *Action[M, S]
}

// keys are the bindings of a kind or a place.
type keys[M, S any] []binding[M, S]

// bind adds a binding of keys to a. help is how the keys show in help, such as "j/k"; empty
// shows the first key. desc is the binding's text in help; empty marks a second key of the
// binding before it, which its help already covers.
func (ks *keys[M, S]) bind(a *Action[M, S], help, desc string, keys ...string) {
	if help == "" && len(keys) > 0 {
		help = keys[0]
	}
	*ks = append(*ks, binding[M, S]{Binding: key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc)), action: a})
}

func (ks keys[M, S]) bindings() []key.Binding {
	out := make([]key.Binding, len(ks))
	for i, b := range ks {
		out[i] = b.Binding
	}
	return out
}

func (ks keys[M, S]) on(s S) []Key[M] {
	out := make([]Key[M], len(ks))
	for i, b := range ks {
		out[i] = Key[M]{Binding: b.Binding, Action: bind(b.action, s)}
	}
	return out
}

// Keyed is a kind or a place, with the keys bound on it.
type Keyed interface {
	Bindings() []key.Binding
}

// made is every kind and place, in the order they were made.
var made []Keyed

// Made is every kind and place made so far, for a check that help shows each one's keys.
func Made() []Keyed { return made }

// Kind is a kind of subject, every action on it, and the keys bound to them.
type Kind[M, S any] struct {
	label   func(S) string
	actions []*Action[M, S]
	keys    keys[M, S]
	via     []func(S) []Group[M]
}

// NewKind defines a kind: a label for one of its subjects in a menu's heading, such as "Commit
// b194130", and its actions.
func NewKind[M, S any](label func(S) string, actions ...*Action[M, S]) *Kind[M, S] {
	k := &Kind[M, S]{label: label, actions: actions}
	made = append(made, k)
	return k
}

// Bind binds keys to a, one of the kind's actions, wherever a subject of the kind is.
func (k *Kind[M, S]) Bind(a *Action[M, S], help, desc string, keys ...string) {
	if !slices.Contains(k.actions, a) {
		panic("interact: binding " + a.Title + " on a kind that does not have it; list it, or BindQuiet it")
	}
	k.keys.bind(a, help, desc, keys...)
}

// BindQuiet binds keys to a, which the kind does not offer, so they run it but neither menus,
// hints, nor help show it: a key that only says why it does nothing here, which keeps it from
// reaching what the subject carries.
func (k *Kind[M, S]) BindQuiet(a *Action[M, S], keys ...string) {
	k.keys.bind(a, "", "", keys...)
}

// Bindings are the kind's keys, for help.
func (k *Kind[M, S]) Bindings() []key.Binding { return k.keys.bindings() }

// Via makes the actions and keys of kind of, on the subject get returns, available on k's
// subjects, after k's own: a line's file, and the file's commit. get must return a T for a kind of
// T, so a projection to files cannot be paired with commits.
func Via[M, S, T any](k *Kind[M, S], get func(S) (T, bool), of *Kind[M, T]) {
	k.via = append(k.via, func(s S) []Group[M] {
		t, ok := get(s)
		if !ok {
			return nil
		}
		return of.groups(t)
	})
}

// groups are a subject's actions and keys, its own first, then those it carries.
func (k *Kind[M, S]) groups(s S) []Group[M] {
	own := Group[M]{Label: k.label(s), keys: k.keys.on(s), from: k}
	for _, a := range k.actions {
		own.Actions = append(own.Actions, bind(a, s))
	}
	out := []Group[M]{own}
	for _, v := range k.via {
		out = append(out, v(s)...)
	}
	return out
}

// Group is one subject's actions, bound to it, under its label.
type Group[M any] struct {
	Label   string
	Actions []Bound[M]
	keys    []Key[M]
	// from is the kind or place the group's keys were bound on.
	from any
}

// Bound is an action bound to its subject, ready to run.
type Bound[M any] struct {
	Title   string
	Help    string
	Applies func(M) bool
	Run     func(M) tea.Cmd
	// action is the action bound, by address.
	action any
}

// Is reports whether b is a, bound to its subject.
func (b Bound[M]) Is(a any) bool { return b.action == a }

// Same reports whether b and o are the same action.
func (b Bound[M]) Same(o Bound[M]) bool { return b.action == o.action }

func bind[M, S any](a *Action[M, S], s S) Bound[M] {
	help := a.Help
	if help == "" {
		help = a.Title
	}
	return Bound[M]{
		Title:   a.Title,
		Help:    help,
		Applies: func(m M) bool { return a.applies(m, s) },
		Run:     func(m M) tea.Cmd { return a.Run(m, s) },
		action:  a,
	}
}

// Key is a binding, bound to a subject: what its keys run there.
type Key[M any] struct {
	key.Binding
	Action Bound[M]
}

// Place is where subjects of kind S are found: a list you move through. A place can have actions and keys of its own, for what its subjects do there, as
// a sidebar entry opens in the main pane.
type Place[M, S any] struct {
	kind     *Kind[M, S]
	onCursor *Action[M, S]
	actions  []*Action[M, S]
	keys     keys[M, S]
}

// Option configures a place.
type Option[M, S any] func(*Place[M, S])

// OnCursor runs a when the cursor lands on a subject of the list. A handler need not be one of the
// kind's actions: showing a commit when the cursor lands on it belongs to the sidebar's list, not to
// commits everywhere.
func OnCursor[M, S any](a *Action[M, S]) Option[M, S] {
	return func(p *Place[M, S]) { p.onCursor = a }
}

// List registers a place you move through, whose entries are subjects of k.
func List[M, S any](k *Kind[M, S], opts ...Option[M, S]) *Place[M, S] {
	p := &Place[M, S]{kind: k}
	for _, o := range opts {
		o(p)
	}
	made = append(made, p)
	return p
}

// Bind binds keys to a on the place's subjects, before the kind's keys. The kind need not have
// a.
func (p *Place[M, S]) Bind(a *Action[M, S], help, desc string, keys ...string) {
	if !slices.Contains(p.actions, a) && !slices.Contains(p.kind.actions, a) {
		p.actions = append(p.actions, a)
	}
	p.keys.bind(a, help, desc, keys...)
}

// Bindings are the place's own keys, for help.
func (p *Place[M, S]) Bindings() []key.Binding { return p.keys.bindings() }

// Target is a subject in its place.
func (p *Place[M, S]) Target(s S) Target[M] {
	groups := p.kind.groups(s)
	// The place's own actions and keys come first, with the subject's.
	var own []Bound[M]
	for _, a := range p.actions {
		own = append(own, bind(a, s))
	}
	groups[0].Actions = append(own, groups[0].Actions...)
	groups[0].keys = append(p.keys.on(s), groups[0].keys...)
	t := Target[M]{place: p, groups: groups}
	if p.onCursor != nil {
		b := bind(p.onCursor, s)
		t.onCursor = &b
	}
	return t
}

// Target is a subject in its place, with its actions and keys, the types erased.
type Target[M any] struct {
	place    any
	onCursor *Bound[M]
	groups   []Group[M]
}

// OnCursor is what runs when the cursor lands on the target, if anything.
func (t Target[M]) OnCursor(m M) tea.Cmd {
	if t.onCursor == nil || !t.onCursor.Applies(m) {
		return nil
	}
	return t.onCursor.Run(m)
}

// Involves reports whether keys bound on kp, a kind or a place, reach the target.
func (t Target[M]) Involves(kp any) bool {
	if t.place == kp {
		return true
	}
	return slices.ContainsFunc(t.groups, func(g Group[M]) bool { return g.from == kp })
}

// Lookup is what msg runs on a chain of targets, the most specific first, such as the cursor's
// subject, then the pane, then the app: the first binding of msg whose action applies, on the
// place, then on the subject's kind, then on what it carries. A subject that takes a key keeps
// it from those after it.
func Lookup[M any](m M, msg tea.KeyPressMsg, chain ...Target[M]) (Bound[M], bool) {
	for _, t := range chain {
		for _, g := range t.groups {
			for _, k := range g.keys {
				if key.Matches(msg, k.Binding) && k.Action.Applies(m) {
					return k.Action, true
				}
			}
		}
	}
	return Bound[M]{}, false
}

// Keys are the bindings that run something on a chain of targets, in the order Lookup tries
// them, each with only the keys that reach it: a binding whose keys a binding before it takes
// shows the rest, as a file's "c/C" shows "C" on a line that takes c, or is left out with none.
func Keys[M any](m M, chain ...Target[M]) []Key[M] {
	taken := map[string]bool{}
	var out []Key[M]
	for _, t := range chain {
		for _, g := range t.groups {
			for _, k := range g.keys {
				if !k.Action.Applies(m) {
					continue
				}
				var left []string
				for _, s := range k.Keys() {
					if !taken[s] {
						left = append(left, s)
					}
				}
				if len(left) == 0 {
					continue
				}
				if len(left) < len(k.Keys()) {
					k.Binding = key.NewBinding(key.WithKeys(left...), key.WithHelp(strings.Join(left, "/"), k.Help().Desc))
				}
				for _, s := range left {
					taken[s] = true
				}
				out = append(out, k)
			}
		}
	}
	return out
}
