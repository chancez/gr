package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// styles are the TUI's colors, modelled on GitHub's light and dark themes.
type styles struct {
	isDark      bool
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
	paneBorder   lipgloss.Style
	badgeOpen    lipgloss.Style
	badgeDone    lipgloss.Style
	badgeWarn    lipgloss.Style
	composeBox   lipgloss.Style
	helpKey      lipgloss.Style
	helpDesc     lipgloss.Style
}

// newStyles picks colors for a dark or light terminal background.
//
// TODO: make colors configurable once there is a config file.
func newStyles(isDark bool) styles {
	ld := lipgloss.LightDark(isDark)
	fg := ld(lipgloss.Color("#1f2328"), lipgloss.Color("#e6edf3"))
	muted := ld(lipgloss.Color("#59636e"), lipgloss.Color("#9198a1"))
	blue := ld(lipgloss.Color("#0969da"), lipgloss.Color("#4493f8"))
	green := ld(lipgloss.Color("#1a7f37"), lipgloss.Color("#3fb950"))
	red := ld(lipgloss.Color("#d1242f"), lipgloss.Color("#f85149"))
	yellow := ld(lipgloss.Color("#9a6700"), lipgloss.Color("#d29922"))
	purple := ld(lipgloss.Color("#8250df"), lipgloss.Color("#ab7df8"))
	border := ld(lipgloss.Color("#d1d9e0"), lipgloss.Color("#3d444d"))
	return styles{
		isDark:      isDark,
		fg:          fg,
		accent:      blue,
		addedBg:     ld(lipgloss.Color("#e6ffec"), lipgloss.Color("#12261e")),
		deletedBg:   ld(lipgloss.Color("#ffebe9"), lipgloss.Color("#25171c")),
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
		paneBorder:   lipgloss.NewStyle().Foreground(border),
		badgeOpen:    lipgloss.NewStyle().Foreground(green),
		badgeDone:    lipgloss.NewStyle().Foreground(purple),
		badgeWarn:    lipgloss.NewStyle().Foreground(yellow),
		composeBox:   lipgloss.NewStyle().Border(lipgloss.ASCIIBorder()).BorderForeground(blue).Padding(0, 1),
		helpKey:      lipgloss.NewStyle().Bold(true).Foreground(blue),
		helpDesc:     lipgloss.NewStyle().Foreground(fg),
	}
}
