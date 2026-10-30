package tui

import "charm.land/bubbles/v2/key"

// keyMap holds every binding, so help text and future remapping read from one place.
type keyMap struct {
	Up           key.Binding
	Down         key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	Top          key.Binding
	Bottom       key.Binding
	Left         key.Binding
	Right        key.Binding
	NextFile     key.Binding
	PrevFile     key.Binding
	AllChanges   key.Binding
	NextCommit   key.Binding
	PrevCommit   key.Binding
	NextHunk     key.Binding
	PrevHunk     key.Binding
	NextThread   key.Binding
	PrevThread   key.Binding
	Focus        key.Binding
	FocusBack    key.Binding
	SectionDown  key.Binding
	SectionUp    key.Binding
	Open         key.Binding
	Select       key.Binding
	Comment      key.Binding
	FileComment  key.Binding
	Reply        key.Binding
	Resolve      key.Binding
	Address      key.Binding
	Edit         key.Binding
	Delete       key.Binding
	Reviewed     key.Binding
	CommitDone   key.Binding
	Submit       key.Binding
	Discard      key.Binding
	Split        key.Binding
	Reload       key.Binding
	Help         key.Binding
	Quit         key.Binding
	Cancel       key.Binding

	// Compose mode.
	Save         key.Binding
	ExternalEdit key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k/up", "up")),
		Down:         key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j/down", "down")),
		HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u", "u"), key.WithHelp("u", "half page up")),
		HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d", "d"), key.WithHelp("d", "half page down")),
		PageUp:       key.NewBinding(key.WithKeys("pgup", "ctrl+b", "b"), key.WithHelp("b/pgup", "page up")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown", "ctrl+f", "f", "space"), key.WithHelp("f/pgdn", "page down")),
		Top:          key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:       key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		Left:         key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/l", "sidebar/diff, or diff side")),
		Right:        key.NewBinding(key.WithKeys("l", "right")),
		NextFile:     key.NewBinding(key.WithKeys("]"), key.WithHelp("]/[", "next/prev file")),
		PrevFile:     key.NewBinding(key.WithKeys("[")),
		AllChanges:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "all changes and back")),
		NextCommit:   key.NewBinding(key.WithKeys("}"), key.WithHelp("}/{", "next/prev commit")),
		PrevCommit:   key.NewBinding(key.WithKeys("{")),
		NextHunk:     key.NewBinding(key.WithKeys("J"), key.WithHelp("J/K", "next/prev hunk")),
		PrevHunk:     key.NewBinding(key.WithKeys("K")),
		NextThread:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n/N", "next/prev thread")),
		PrevThread:   key.NewBinding(key.WithKeys("N")),
		Focus:        key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
		FocusBack:    key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous pane")),
		SectionDown:  key.NewBinding(key.WithKeys("ctrl+j"), key.WithHelp("ctrl+j/k", "files/changes list")),
		SectionUp:    key.NewBinding(key.WithKeys("ctrl+k")),
		Open:         key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Select:       key.NewBinding(key.WithKeys("v", "V"), key.WithHelp("v", "select lines")),
		Comment:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment")),
		FileComment:  key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "comment on file/commit")),
		Reply:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reply")),
		Resolve:      key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "resolve/reopen")),
		Address:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "mark addressed")),
		Edit:         key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit my comment")),
		Delete:       key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "delete my comment")),
		Reviewed:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mark file/commit reviewed")),
		CommitDone:   key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "mark commit reviewed")),
		Submit:       key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "submit review")),
		Discard:      key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "discard draft")),
		Split:        key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "split/unified")),
		Reload:       key.NewBinding(key.WithKeys("R", "ctrl+r"), key.WithHelp("R", "reload")),
		Help:         key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:         key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Cancel:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Save:         key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		ExternalEdit: key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "open $EDITOR")),
	}
}

// ShortHelp is shown in the status bar.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Comment, k.Reply, k.Resolve, k.Reviewed, k.Submit, k.Focus, k.Help, k.Quit}
}

// FullHelp is shown with ?.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.HalfPageDown, k.HalfPageUp, k.PageDown, k.PageUp, k.Top, k.Bottom},
		{k.AllChanges, k.NextCommit, k.NextFile, k.NextHunk, k.NextThread, k.Left, k.Focus, k.FocusBack, k.SectionDown, k.Open, k.Split, k.Reload},
		{k.Select, k.Comment, k.FileComment, k.Reply, k.Resolve, k.Address, k.Edit, k.Delete},
		{k.Reviewed, k.CommitDone, k.Submit, k.Discard, k.Save, k.ExternalEdit, k.Cancel, k.Help, k.Quit},
	}
}
