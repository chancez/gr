package tui

import (
	"fmt"
	"image/color"
	"log/slog"
	"strings"
	"time"

	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/look"
	"github.com/charmbracelet/x/ansi"
)

func (m *model) setStatus(format string, args ...any) {
	m.status, m.statusErr, m.statusAt = fmt.Sprintf(format, args...), false, time.Now()
}

func (m *model) setError(err error) {
	slog.Warn("tui error", "err", err)
	m.status, m.statusErr, m.statusAt = err.Error(), true, time.Now()
}

// expireStatus clears a message once it has been shown long enough. A pending two-key command
// keeps its prompt.
func (m *model) expireStatus(now time.Time) {
	if m.status == "" || m.pending != "" {
		return
	}
	ttl := statusTTL
	if m.statusErr {
		ttl = errorTTL
	}
	if now.Sub(m.statusAt) >= ttl {
		m.status = ""
	}
}

func (m *model) renderStatus() string {
	th := m.r.th
	switch m.mode {
	case modeSearch:
		return m.searchInput.View()
	case modeVerdict:
		t := theVerdict.Target(verdictSubject{})
		spans := append([]look.Span{look.Heading("Submit review: ")}, m.controls("verdict", modeVerdict, t,
			button(actVerdictApprove, "Approve"),
			button(actVerdictRequestChanges, "Request changes"),
			button(actVerdictComment, "Comment"),
			button(actVerdictCancel, "Cancel"))...)
		spans = append(append(spans, look.Default("  ")), look.Hint("A R C", "skip the summary")...)
		return th.DrawCut(look.Plain, m.width, spans...)
	case modeConfirm:
		spans := append([]look.Span{look.Error(m.confirm.prompt), look.Default("  ")}, m.controls("confirm", modeConfirm, theConfirm.Target(confirmSubject{}),
			button(actConfirmYes, "Yes"),
			button(actConfirmNo, "No"))...)
		return th.DrawCut(look.Plain, m.width, spans...)
	case modeCompose:
		if m.compose.closing {
			spans := append([]look.Span{look.Warning("Unsaved comment: ")}, m.controls("closing", modeCompose, theClosing.Target(composeSubject{}),
				button(actComposeSave, "Save"),
				button(actClosingDiscard, "Discard"),
				button(actClosingKeep, "Keep editing"))...)
			return th.DrawCut(look.Plain, m.width, spans...)
		}
	}
	d := m.data
	text := fmt.Sprintf("#%d rev %d", d.change.Seq, d.revision.Seq)
	if d.change.State != store.ChangeOpen {
		text += " " + string(d.change.State)
	}
	left := []look.Span{look.Text(text)}
	if _, ok := d.draft(); ok {
		left = append(left, look.Draft(" draft"))
	}
	left = append(left, look.Text(" "+m.reviewProgress()))
	if m.autoAdvance {
		left = append(left, look.Mode(" auto-advance"))
	}
	if m.split {
		left = append(left, look.Text(" split"))
	}
	if m.main.anchor >= 0 {
		left = append(left, look.Mode(" selecting"))
	}
	if label := m.searchLabel(); label != "" {
		left = append(left, look.Text(" "), look.Styled(func(color.Color) string { return th.Render(look.SearchMatch, look.Text(label)) }),
			look.Text(" "))
		left = append(left, look.Hint("esc", "clears")...)
	}
	l := th.Render(look.Plain, left...)
	if m.mode == modeCompose {
		// Keys go to the comment box, whose controls are its hints.
		return look.Fit(l, m.width)
	}
	// Fit the hints in what the left side leaves, keeping help and quit.
	right := m.renderHints(m.shortHelp(), max(m.width-ansi.StringWidth(l)-2, 10), 3)
	gap := max(m.width-ansi.StringWidth(l)-ansi.StringWidth(right)-1, 1)
	return look.Fit(l+strings.Repeat(" ", gap)+right, m.width)
}
