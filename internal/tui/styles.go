package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// styles are the TUI's colors, modelled on GitHub's dark theme.
type styles struct {
	addedBg    color.Color
	deletedBg  color.Color
	cursorBg   color.Color
	selectedBg color.Color

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
}

func defaultStyles() styles {
	fg := lipgloss.Color("#e6edf3")
	muted := lipgloss.Color("#7d8590")
	blue := lipgloss.Color("#4493f8")
	green := lipgloss.Color("#3fb950")
	red := lipgloss.Color("#f85149")
	yellow := lipgloss.Color("#d29922")
	purple := lipgloss.Color("#ab7df8")
	border := lipgloss.Color("#30363d")
	return styles{
		addedBg:    lipgloss.Color("#12261e"),
		deletedBg:  lipgloss.Color("#25171c"),
		cursorBg:   lipgloss.Color("#1f3a5f"),
		selectedBg: lipgloss.Color("#2a2f45"),

		gutter:       lipgloss.NewStyle().Foreground(muted),
		addedMark:    lipgloss.NewStyle().Foreground(green),
		deletedMark:  lipgloss.NewStyle().Foreground(red),
		fileHeader:   lipgloss.NewStyle().Bold(true).Foreground(fg).Background(lipgloss.Color("#161b22")),
		hunkHeader:   lipgloss.NewStyle().Foreground(blue).Background(lipgloss.Color("#121d2f")),
		commitHeader: lipgloss.NewStyle().Bold(true).Foreground(yellow),
		heading:      lipgloss.NewStyle().Bold(true).Foreground(fg),
		muted:        lipgloss.NewStyle().Foreground(muted),
		threadBorder: lipgloss.NewStyle().Border(lipgloss.ASCIIBorder()).BorderForeground(border).Padding(0, 1),
		threadTitle:  lipgloss.NewStyle().Bold(true),
		author:       lipgloss.NewStyle().Bold(true).Foreground(purple),
		statusBar:    lipgloss.NewStyle().Foreground(muted),
		statusErr:    lipgloss.NewStyle().Foreground(red),
		navSelected:  lipgloss.NewStyle().Background(lipgloss.Color("#21262d")),
		navFocused:   lipgloss.NewStyle().Background(lipgloss.Color("#1f3a5f")),
		navHeader:    lipgloss.NewStyle().Bold(true).Foreground(fg),
		paneBorder:   lipgloss.NewStyle().Foreground(border),
		badgeOpen:    lipgloss.NewStyle().Foreground(green),
		badgeDone:    lipgloss.NewStyle().Foreground(purple),
		badgeWarn:    lipgloss.NewStyle().Foreground(yellow),
		composeBox:   lipgloss.NewStyle().Border(lipgloss.ASCIIBorder()).BorderForeground(blue).Padding(0, 1),
	}
}
