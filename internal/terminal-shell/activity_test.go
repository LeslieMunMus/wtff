package terminalshell

import (
	"strings"
	"testing"
	"time"
)

func TestActivityIndicatorTicksAdvanceFrameCount(t *testing.T) {
	a := newActivityIndicator("Working")
	if a.ticks != 0 {
		t.Fatalf("ticks = %d, want 0 at construction", a.ticks)
	}
	updated, cmd := a.update(activityTickMsg{})
	if updated.ticks != 1 {
		t.Fatalf("ticks after one update = %d, want 1", updated.ticks)
	}
	if cmd == nil {
		t.Fatal("update should reschedule the next tick")
	}
}

// Anything other than its own tick message must be a no-op, so a caller can
// route every message through it unconditionally, the same contract
// selectList's update already follows.
func TestActivityIndicatorIgnoresOtherMessages(t *testing.T) {
	a := newActivityIndicator("Working")
	updated, cmd := a.update(planReadyMsg{})
	if updated.ticks != 0 || cmd != nil {
		t.Fatalf("unrelated message changed state: ticks=%d cmd=%v", updated.ticks, cmd)
	}
}

// The view must show the label and a live elapsed time, not a static
// string, since a static line during unbounded work is the exact defect
// that prompted building this widget: it looks the same whether the
// operation is progressing or completely hung.
func TestActivityIndicatorViewShowsLabelAndElapsed(t *testing.T) {
	a := newActivityIndicator("Scanning")
	a.startedAt = time.Now().Add(-90 * time.Second)
	view := a.view(darkTheme)
	if !strings.Contains(view, "Scanning") {
		t.Fatal("view should show the label")
	}
	if !strings.Contains(view, "1m30s") {
		t.Fatalf("view should show elapsed time, got: %s", view)
	}
}
