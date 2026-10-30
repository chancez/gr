package tui

import "testing"

// linePane is a pane of n one-line rows, all selectable.
func linePane(n, height int) *pane {
	p := &pane{anchor: -1, height: height, scrolloff: 3}
	for range n {
		p.rows = append(p.rows, row{kind: rowLine, file: 0})
	}
	p.layout(func(int, rowState) []string { return []string{""} })
	return p
}

func TestScrolloff(t *testing.T) {
	p := linePane(100, 20)
	p.move(19)
	if got := p.cursor - p.offset; got != 20-1-3 {
		t.Errorf("moving down: cursor %d lines from the top, want %d", got, 20-1-3)
	}
	p.jump(50)
	if got := p.cursor - p.offset; got != 3 {
		t.Errorf("after a jump: cursor %d lines from the top, want 3", got)
	}
	p.move(-1)
	if got := p.cursor - p.offset; got != 3 {
		t.Errorf("moving up: cursor %d lines from the top, want 3", got)
	}
	p.top()
	if p.offset != 0 || p.cursor != 0 {
		t.Errorf("top: offset %d cursor %d", p.offset, p.cursor)
	}
	p.jump(99)
	if p.offset != 80 {
		t.Errorf("jump to the last row: offset %d, want 80", p.offset)
	}

	// A small pane shrinks the margin rather than jittering.
	small := linePane(100, 4)
	small.jump(50)
	if got := small.cursor - small.offset; got != 1 {
		t.Errorf("small pane: cursor %d lines from the top, want 1", got)
	}
}
