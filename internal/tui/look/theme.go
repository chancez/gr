// Package look is the TUI's registry of ADR 8. A theme assigns the palette's colors to roles, for a
// light or dark background, and styles every element and surface from the roles.
//
// Code that draws builds lines of spans and says which surface they sit on. A span is made only by
// this package's constructors, each for one kind of value, such as Author or SHA, so a value is
// always drawn as its own element. Elements are unexported, so nothing else can pair a value with
// the wrong one.
package look

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// roles are what a theme's colors are for. Elements and surfaces are styled from roles, so a theme
// that changes what danger looks like changes it everywhere.
type roles struct {
	text   color.Color
	muted  color.Color
	accent color.Color
	// success, danger, attention, and done are for states: open or approved, errors and requested
	// changes, things waiting on someone, and things finished.
	success   color.Color
	danger    color.Color
	attention color.Color
	done      color.Color
	// added and deleted mark diff lines.
	added   color.Color
	deleted color.Color
	border  color.Color
	// onSolid is text on a solid background, such as the focused pane's title.
	onSolid color.Color

	headingBg       color.Color
	hunkBg          color.Color
	addedBg         color.Color
	deletedBg       color.Color
	cursorBg        color.Color
	dimCursorBg     color.Color
	selectedBg      color.Color
	paneUnfocusedBg color.Color
	controlBg       color.Color
	commentedBg     color.Color
}

// The built-in themes.
const (
	// ThemeDefault is modelled on GitHub's light and dark themes.
	ThemeDefault = "default"
	// ThemeColorblind is modelled on GitHub's protanopia and deuteranopia themes: blue for added
	// and success, orange for deleted and danger, in place of green and red.
	ThemeColorblind = "colorblind"
)

// Themes lists the built-in themes.
var Themes = []string{ThemeDefault, ThemeColorblind}

// newRoles assigns the palette to roles, in the named theme for a light or dark background.
func newRoles(isDark bool, theme string) roles {
	ld := lipgloss.LightDark(isDark)
	r := roles{
		text:            ld(ink, snow),
		muted:           ld(slate, ash),
		accent:          ld(blue, lightBlue),
		success:         ld(green, lightGreen),
		danger:          ld(red, lightRed),
		attention:       ld(amber, gold),
		done:            ld(purple, lavender),
		added:           ld(green, lightGreen),
		deleted:         ld(red, lightRed),
		border:          ld(paleGray, charcoal),
		onSolid:         ld(white, night),
		headingBg:       ld(offWhite, nearNight),
		hunkBg:          ld(paleBlue, navy),
		addedBg:         ld(paleGreen, darkGreen),
		deletedBg:       ld(palePink, darkMaroon),
		cursorBg:        ld(skyBlue, steelBlue),
		dimCursorBg:     ld(lightGray, graphite),
		selectedBg:      ld(paleYellow, indigoGray),
		paneUnfocusedBg: ld(lightGray, darkGray),
		controlBg:       ld(paleGray, charcoal),
		commentedBg:     ld(sand, darkOlive),
	}
	if theme == ThemeColorblind {
		r.success, r.added = r.accent, r.accent
		r.danger = ld(orange, lightOrange)
		r.deleted = r.danger
		r.addedBg = ld(paleSky, deepNavy)
		r.deletedBg = ld(paleOrange, darkBrown)
	}
	return r
}

// element is a kind of text on screen. Its style sets a foreground and attributes, never a
// background, which comes from the surface the text sits on.
type element int

const (
	// elDefault is text in the terminal's own foreground.
	elDefault element = iota
	elText
	elMuted
	elHeading
	elAuthor
	elSHA
	elPath
	elRef
	elGutter
	elMarkAdded
	elMarkDeleted
	elHunkRange
	elKey
	elKeyDesc
	elCount
	elReviewed
	elDraft
	elStateOpen
	elStateAddressed
	elStateResolved
	elOutdated
	elVerdictApprove
	elVerdictRequestChanges
	elVerdictComment
	elError
	elBorder
	elBorderActive
	// elMode marks a mode that changes what keys do, such as selecting lines.
	elMode
	elWarning
	// elButton is a control's label.
	elButton
	// elFill is blank room between and after controls, which a border they sit in fills with its
	// line.
	elFill
	numElements
)

// elementNames are the names the config file will override elements by.
var elementNames = [numElements]string{
	elDefault:               "default",
	elText:                  "text",
	elMuted:                 "muted",
	elHeading:               "heading",
	elAuthor:                "author",
	elSHA:                   "sha",
	elPath:                  "path",
	elRef:                   "ref",
	elGutter:                "gutter",
	elMarkAdded:             "mark.added",
	elMarkDeleted:           "mark.deleted",
	elHunkRange:             "hunk-range",
	elKey:                   "key",
	elKeyDesc:               "key-desc",
	elCount:                 "count",
	elReviewed:              "reviewed",
	elDraft:                 "draft",
	elStateOpen:             "thread-state.open",
	elStateAddressed:        "thread-state.addressed",
	elStateResolved:         "thread-state.resolved",
	elOutdated:              "outdated",
	elVerdictApprove:        "verdict.approve",
	elVerdictRequestChanges: "verdict.request-changes",
	elVerdictComment:        "verdict.comment",
	elError:                 "error",
	elBorder:                "border",
	elBorderActive:          "border.active",
	elMode:                  "mode",
	elWarning:               "warning",
	elButton:                "button",
	elFill:                  "fill",
}

// Prop is an option a place asks an element for. The theme defines what each does.
type Prop uint8

const (
	// Dim is for entries that are done: reviewed, collapsed, or resolved.
	Dim Prop = 1 << iota
	// Strong is for emphasis, such as in headers.
	Strong
	// Underlined is for what a click runs that has no chip, as a hint's key or a lit link.
	Underlined
)

// Surface is where a line sits. It owns the background.
type Surface int

const (
	Plain Surface = iota
	HeadingBar
	HunkBar
	Cursor
	CursorDim
	Selected
	AddedLine
	DeletedLine
	ActiveHunk
	PaneFocused
	PaneUnfocused
	Status
	StatusError
	SearchMatch
	// Control is a control's own background, a chip on whatever it sits on, as GitHub's buttons.
	Control
	// ControlSolid is a one-cell control that has to stand out on any line, as the + that comments,
	// a + on the solid accent, as GitHub's blue square.
	ControlSolid
	// Commented is the line numbers of lines a thread is on, as GitHub highlights a range's.
	Commented
	numSurfaces
)

var surfaceNames = [numSurfaces]string{
	Plain:         "surface.plain",
	HeadingBar:    "surface.heading",
	HunkBar:       "surface.hunk",
	Cursor:        "surface.cursor",
	CursorDim:     "surface.cursor-dim",
	Selected:      "surface.selected",
	AddedLine:     "surface.added",
	DeletedLine:   "surface.deleted",
	ActiveHunk:    "surface.active-hunk",
	PaneFocused:   "surface.pane-focused",
	PaneUnfocused: "surface.pane-unfocused",
	Status:        "surface.status",
	StatusError:   "surface.status-error",
	SearchMatch:   "surface.search-match",
	Control:       "surface.control",
	ControlSolid:  "surface.control-solid",
	Commented:     "surface.commented",
}

// surfaceStyle is how a surface draws. A soft surface keeps its elements' colors. A solid one
// draws them all in its own foreground, bold when it is, since colored text on a solid color is
// unreadable.
type surfaceStyle struct {
	bg    color.Color
	solid bool
	fg    color.Color
	bold  bool
}

// Theme is the registry: the roles a theme assigns for a light or dark background, and the style
// they give each element and surface.
type Theme struct {
	isDark   bool
	roles    roles
	elements [numElements]lipgloss.Style
	surfaces [numSurfaces]surfaceStyle
	glyphs   Glyphs
}

// New is the named theme, one of Themes, for a light or dark background.
func New(isDark bool, name string) *Theme {
	p := newRoles(isDark, name)
	ld := lipgloss.LightDark(isDark)
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	bold := func(c color.Color) lipgloss.Style { return fg(c).Bold(true) }
	t := &Theme{isDark: isDark, roles: p}
	t.elements = [numElements]lipgloss.Style{
		elDefault:               lipgloss.NewStyle(),
		elText:                  fg(p.text),
		elMuted:                 fg(p.muted),
		elHeading:               bold(p.text),
		elAuthor:                bold(ld(purple, lavender)),
		elSHA:                   fg(ld(amber, gold)),
		elPath:                  bold(p.text),
		elRef:                   bold(p.text),
		elGutter:                fg(p.muted),
		elMarkAdded:             fg(p.added),
		elMarkDeleted:           fg(p.deleted),
		elHunkRange:             fg(p.accent),
		elKey:                   bold(p.accent),
		elKeyDesc:               fg(p.text),
		elCount:                 fg(p.accent),
		elReviewed:              fg(p.success),
		elDraft:                 fg(p.attention),
		elStateOpen:             fg(p.success),
		elStateAddressed:        fg(p.attention),
		elStateResolved:         fg(p.done),
		elOutdated:              fg(p.attention),
		elVerdictApprove:        bold(p.success),
		elVerdictRequestChanges: bold(p.danger),
		elVerdictComment:        bold(p.text),
		elError:                 fg(p.danger),
		elBorder:                fg(p.border),
		elBorderActive:          fg(p.accent),
		elMode:                  fg(p.attention),
		elWarning:               bold(p.attention),
		elButton:                bold(p.text),
		elFill:                  lipgloss.NewStyle(),
	}
	t.surfaces = [numSurfaces]surfaceStyle{
		Plain:         {},
		HeadingBar:    {bg: p.headingBg},
		HunkBar:       {bg: p.hunkBg},
		Cursor:        {bg: p.cursorBg},
		CursorDim:     {bg: p.dimCursorBg},
		Selected:      {bg: p.selectedBg},
		AddedLine:     {bg: p.addedBg},
		DeletedLine:   {bg: p.deletedBg},
		ActiveHunk:    {bg: p.accent, solid: true, fg: p.onSolid},
		PaneFocused:   {bg: p.accent, solid: true, fg: p.onSolid, bold: true},
		PaneUnfocused: {bg: p.paneUnfocusedBg, solid: true, fg: p.muted, bold: true},
		Status:        {bg: p.success, solid: true, fg: p.onSolid, bold: true},
		StatusError:   {bg: p.danger, solid: true, fg: p.onSolid, bold: true},
		SearchMatch:   {bg: p.attention, solid: true, fg: p.onSolid, bold: true},
		Control:       {bg: p.controlBg},
		ControlSolid:  {bg: p.accent, solid: true, fg: p.onSolid, bold: true},
		Commented:     {bg: p.commentedBg},
	}
	return t
}

// style is how el with props draws on surface s.
func (t *Theme) style(el element, p Prop, s Surface) lipgloss.Style {
	ss := t.surfaces[s]
	if ss.solid {
		return lipgloss.NewStyle().Foreground(ss.fg).Background(ss.bg).Bold(ss.bold || p&Strong != 0)
	}
	st := t.elements[el]
	if p&Dim != 0 {
		st = st.Foreground(t.roles.muted).Bold(false)
	}
	if p&Strong != 0 {
		st = st.Bold(true)
	}
	if p&Underlined != 0 {
		st = st.Underline(true)
	}
	if ss.bg != nil {
		st = st.Background(ss.bg)
	}
	return st
}
