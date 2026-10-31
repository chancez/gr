package tui

import (
	"fmt"
	"image/color"
	"log/slog"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
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
		return th.Render(look.Plain, look.Heading("Submit review: ")) + m.renderHints([]key.Binding{
			hint("a", "approve"), hint("r", "request changes"), hint("c", "comment"),
			hint("A R C", "skip the summary"), hint("esc", "cancel"),
		}, m.width-15)
	case modeConfirm:
		return th.Render(look.Plain, look.Error(m.confirm.prompt))
	case modeCompose:
		if m.compose.closing {
			return th.Render(look.Plain, look.Warning("Unsaved comment: ")) + m.renderHints([]key.Binding{
				hint("s", "save it"), hint("d", "discard it"), hint("esc", "keep editing"),
			}, m.width-17)
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
	// Fit the hints in what the left side leaves, keeping help and quit.
	right := m.renderHints(m.shortHelp(), max(m.width-ansi.StringWidth(l)-2, 10))
	gap := max(m.width-ansi.StringWidth(l)-ansi.StringWidth(right)-1, 1)
	return look.Fit(l+strings.Repeat(" ", gap)+right, m.width)
}
