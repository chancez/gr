package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// styles are the TUI's colors, modelled on GitHub's light and dark themes.
type styles struct {
	isDark bool
	// headingBg is the background of heading bars such as the overview's review headers.
	headingBg   color.Color
	fg          color.Color
	accent      color.Color
	dimCursorBg color.Color
	addedBg     color.Color
	deletedBg   color.Color
	cursorBg    color.Color
	selectedBg  color.Color

	gutter       lipgloss.Style
	addedMark    lipgloss.Style
	deletedMark  lipgloss.Style
	fileHeader   lipgloss.Style
	hunkHeader   lipgloss.Style
	commitHeader lipgloss.Style
	heading      lipgloss.Style
	muted        lipgloss.Style
	threadBorder lipgloss.Style
	threadTitle  lipgloss.Style
	author       lipgloss.Style
	statusBar    lipgloss.Style
	statusErr    lipgloss.Style
	navSelected  lipgloss.Style
	navFocused   lipgloss.Style
	navHeader    lipgloss.Style
	navHeading   lipgloss.Style
	paneBorder   lipgloss.Style
	badgeOpen    lipgloss.Style
	badgeDone    lipgloss.Style
	badgeWarn    lipgloss.Style
	composeBox   lipgloss.Style
	searchMatch  lipgloss.Style
	statusMsg    lipgloss.Style
	statusErrMsg lipgloss.Style
	helpKey      lipgloss.Style
	helpDesc     lipgloss.Style
	// paneFocused and paneUnfocused title panes. The focused title is a solid accent bar, distinct
	// from the softer cursor color.
	paneFocused   lipgloss.Style
	paneUnfocused lipgloss.Style
}

// newStyles picks colors for a dark or light terminal background, in a built-in theme.
//
// TODO: let the config file override individual colors.
func newStyles(isDark bool, theme string) styles {
	ld := lipgloss.LightDark(isDark)
	fg := ld(lipgloss.Color("#1f2328"), lipgloss.Color("#e6edf3"))
	muted := ld(lipgloss.Color("#59636e"), lipgloss.Color("#9198a1"))
	blue := ld(lipgloss.Color("#0969da"), lipgloss.Color("#4493f8"))
	green := ld(lipgloss.Color("#1a7f37"), lipgloss.Color("#3fb950"))
	red := ld(lipgloss.Color("#d1242f"), lipgloss.Color("#f85149"))
	yellow := ld(lipgloss.Color("#9a6700"), lipgloss.Color("#d29922"))
	purple := ld(lipgloss.Color("#8250df"), lipgloss.Color("#ab7df8"))
	border := ld(lipgloss.Color("#d1d9e0"), lipgloss.Color("#3d444d"))
	addedBg := ld(lipgloss.Color("#e6ffec"), lipgloss.Color("#12261e"))
	deletedBg := ld(lipgloss.Color("#ffebe9"), lipgloss.Color("#25171c"))
	if theme == ThemeColorblind {
		green = blue
		red = ld(lipgloss.Color("#bc4c00"), lipgloss.Color("#f0883e"))
		addedBg = ld(lipgloss.Color("#ddf4ff"), lipgloss.Color("#0d2140"))
		deletedBg = ld(lipgloss.Color("#fff1e5"), lipgloss.Color("#2e1a08"))
	}
	return styles{
		isDark:      isDark,
		fg:          fg,
		accent:      blue,
		addedBg:     addedBg,
		deletedBg:   deletedBg,
		cursorBg:    ld(lipgloss.Color("#b6e3ff"), lipgloss.Color("#1f3a5f")),
		dimCursorBg: ld(lipgloss.Color("#eaeef2"), lipgloss.Color("#262c36")),
		selectedBg:  ld(lipgloss.Color("#fff8c5"), lipgloss.Color("#2a2f45")),

		gutter:       lipgloss.NewStyle().Foreground(muted),
		addedMark:    lipgloss.NewStyle().Foreground(green),
		deletedMark:  lipgloss.NewStyle().Foreground(red),
		fileHeader:   lipgloss.NewStyle().Bold(true).Foreground(fg).Background(ld(lipgloss.Color("#f6f8fa"), lipgloss.Color("#161b22"))),
		hunkHeader:   lipgloss.NewStyle().Foreground(blue).Background(ld(lipgloss.Color("#eef6ff"), lipgloss.Color("#121d2f"))),
		commitHeader: lipgloss.NewStyle().Bold(true).Foreground(yellow),
		heading:      lipgloss.NewStyle().Bold(true).Foreground(fg),
		muted:        lipgloss.NewStyle().Foreground(muted),
		threadBorder: lipgloss.NewStyle().Border(lipgloss.ASCIIBorder()).BorderForeground(border).Padding(0, 1),
		threadTitle:  lipgloss.NewStyle().Bold(true).Foreground(fg),
		author:       lipgloss.NewStyle().Bold(true).Foreground(purple),
		statusBar:    lipgloss.NewStyle().Foreground(fg),
		statusErr:    lipgloss.NewStyle().Foreground(red),
		navSelected:  lipgloss.NewStyle().Foreground(fg).Background(ld(lipgloss.Color("#eaeef2"), lipgloss.Color("#262c36"))),
		navFocused:   lipgloss.NewStyle().Foreground(fg).Background(ld(lipgloss.Color("#b6e3ff"), lipgloss.Color("#1f3a5f"))),
		navHeader:    lipgloss.NewStyle().Bold(true).Foreground(fg),
		navHeading:   lipgloss.NewStyle().Bold(true).Foreground(fg).Background(ld(lipgloss.Color("#f6f8fa"), lipgloss.Color("#161b22"))),
		paneBorder:   lipgloss.NewStyle().Foreground(border),
		badgeOpen:    lipgloss.NewStyle().Foreground(green),
		badgeDone:    lipgloss.NewStyle().Foreground(purple),
		badgeWarn:    lipgloss.NewStyle().Foreground(yellow),
		composeBox:   lipgloss.NewStyle().Border(lipgloss.ASCIIBorder()).BorderForeground(blue).Padding(0, 1),
		searchMatch: lipgloss.NewStyle().Bold(true).Background(yellow).
			Foreground(ld(lipgloss.Color("#ffffff"), lipgloss.Color("#0d1117"))),
		statusMsg: lipgloss.NewStyle().Bold(true).Background(green).
			Foreground(ld(lipgloss.Color("#ffffff"), lipgloss.Color("#0d1117"))),
		statusErrMsg: lipgloss.NewStyle().Bold(true).Background(red).
			Foreground(ld(lipgloss.Color("#ffffff"), lipgloss.Color("#0d1117"))),
		helpKey:  lipgloss.NewStyle().Bold(true).Foreground(blue),
		helpDesc: lipgloss.NewStyle().Foreground(fg),
		paneFocused: lipgloss.NewStyle().Bold(true).Background(blue).
			Foreground(ld(lipgloss.Color("#ffffff"), lipgloss.Color("#0d1117"))),
		paneUnfocused: lipgloss.NewStyle().Bold(true).Foreground(muted).
			Background(ld(lipgloss.Color("#eaeef2"), lipgloss.Color("#21262d"))),
	}
}
