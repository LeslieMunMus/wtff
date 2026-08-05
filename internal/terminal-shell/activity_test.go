package terminalshell

import (
	"strings"
	"testing"
	"time"
)

// The spinner must reschedule its own next tick, or the animation runs for
// exactly one frame and stops.
func TestActivityIndicatorTickReschedules(t *testing.T) {
	a := newActivityIndicator("Working")
	_, cmd := a.update(a.spin.Tick())
	if cmd == nil {
		t.Fatal("a tick should reschedule the next tick")
	}
}

// Anything other than the spinner's own tick message must be a no-op, so a
// caller can route every message through it unconditionally, the same
// contract selectList's update already follows.
func TestActivityIndicatorIgnoresOtherMessages(t *testing.T) {
	a := newActivityIndicator("Working")
	before := a.view(brandTheme)
	updated, cmd := a.update(scanDoneMsg{})
	if cmd != nil {
		t.Fatalf("unrelated message produced a command: %v", cmd)
	}
	if updated.view(brandTheme) != before {
		t.Fatal("unrelated message changed the rendered state")
	}
}

// The view must show the label and a live elapsed time, not a static
// string, since a static line during unbounded work is the exact defect
// that prompted building this widget: it looks the same whether the
// operation is progressing or completely hung.
func TestActivityIndicatorViewShowsLabelAndElapsed(t *testing.T) {
	a := newActivityIndicator("Scanning")
	a.startedAt = time.Now().Add(-90 * time.Second)
	view := a.view(brandTheme)
	if !strings.Contains(view, "Scanning") {
		t.Fatal("view should show the label")
	}
	if !strings.Contains(view, "1m30s") {
		t.Fatalf("view should show elapsed time, got: %s", view)
	}
}

// The indicator is one line, deliberately. The first version rendered the
// full multi-line animated mark during operations, which on a real screen
// read as a field of drifting dots and was rejected against the supplied
// reference, a single compact status line. This pins the corrected shape.
func TestActivityIndicatorViewIsASingleLine(t *testing.T) {
	a := newActivityIndicator("Scanning")
	if strings.Contains(a.view(brandTheme), "\n") {
		t.Fatal("the activity indicator must render as a single line")
	}
}

// The spinner glyph must change across ticks, or the line is a static
// string again, indistinguishable from a hang. Driven through the spinner
// component's own tick message, the same way the real event loop drives it,
// rather than by poking internal state.
func TestActivityIndicatorGlyphAnimatesAcrossTicks(t *testing.T) {
	a := newActivityIndicator("Scanning")
	first := a.view(brandTheme)
	a, _ = a.update(a.spin.Tick())
	second := a.view(brandTheme)
	if first == second {
		t.Fatal("consecutive ticks should render different spinner frames")
	}
}
