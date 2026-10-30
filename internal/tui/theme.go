package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// This file is the registry of ADR 8. A theme assigns the palette's colors to roles, for a light or
// dark background, and styles every element and surface from the roles. Code that draws builds
// lines of spans, says which surface they sit on, and leaves the colors to the theme.

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
}

// props are options a place asks an element for. The theme defines what each does.
type props uint8

const (
	// dim is for entries that are done: reviewed, collapsed, or resolved.
	dim props = 1 << iota
	// strong is for emphasis, such as in headers.
	strong
)

// surface is where a line sits. It owns the background.
type surface int

const (
	surfPlain surface = iota
	surfHeading
	surfHunk
	surfCursor
	surfCursorDim
	surfSelected
	surfAdded
	surfDeleted
	surfActiveHunk
	surfPaneFocused
	surfPaneUnfocused
	surfStatus
	surfStatusError
	surfSearchMatch
	numSurfaces
)

var surfaceNames = [numSurfaces]string{
	surfPlain:         "surface.plain",
	surfHeading:       "surface.heading",
	surfHunk:          "surface.hunk",
	surfCursor:        "surface.cursor",
	surfCursorDim:     "surface.cursor-dim",
	surfSelected:      "surface.selected",
	surfAdded:         "surface.added",
	surfDeleted:       "surface.deleted",
	surfActiveHunk:    "surface.active-hunk",
	surfPaneFocused:   "surface.pane-focused",
	surfPaneUnfocused: "surface.pane-unfocused",
	surfStatus:        "surface.status",
	surfStatusError:   "surface.status-error",
	surfSearchMatch:   "surface.search-match",
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

// theme is the registry: the roles a theme assigns for a light or dark background, and the style
// they give each element and surface.
type theme struct {
	isDark   bool
	roles    roles
	elements [numElements]lipgloss.Style
	surfaces [numSurfaces]surfaceStyle
}

func newTheme(isDark bool, name string) *theme {
	p := newRoles(isDark, name)
	ld := lipgloss.LightDark(isDark)
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	bold := func(c color.Color) lipgloss.Style { return fg(c).Bold(true) }
	t := &theme{isDark: isDark, roles: p}
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
	}
	t.surfaces = [numSurfaces]surfaceStyle{
		surfPlain:         {},
		surfHeading:       {bg: p.headingBg},
		surfHunk:          {bg: p.hunkBg},
		surfCursor:        {bg: p.cursorBg},
		surfCursorDim:     {bg: p.dimCursorBg},
		surfSelected:      {bg: p.selectedBg},
		surfAdded:         {bg: p.addedBg},
		surfDeleted:       {bg: p.deletedBg},
		surfActiveHunk:    {bg: p.accent, solid: true, fg: p.onSolid},
		surfPaneFocused:   {bg: p.accent, solid: true, fg: p.onSolid, bold: true},
		surfPaneUnfocused: {bg: p.paneUnfocusedBg, solid: true, fg: p.muted, bold: true},
		surfStatus:        {bg: p.success, solid: true, fg: p.onSolid, bold: true},
		surfStatusError:   {bg: p.danger, solid: true, fg: p.onSolid, bold: true},
		surfSearchMatch:   {bg: p.attention, solid: true, fg: p.onSolid, bold: true},
	}
	return t
}

// style is how el with props draws on surface s.
func (t *theme) style(el element, p props, s surface) lipgloss.Style {
	ss := t.surfaces[s]
	if ss.solid {
		return lipgloss.NewStyle().Foreground(ss.fg).Background(ss.bg).Bold(ss.bold || p&strong != 0)
	}
	st := t.elements[el]
	if p&dim != 0 {
		st = st.Foreground(t.roles.muted).Bold(false)
	}
	if p&strong != 0 {
		st = st.Bold(true)
	}
	if ss.bg != nil {
		st = st.Background(ss.bg)
	}
	return st
}
