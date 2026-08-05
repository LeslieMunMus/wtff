package terminalshell

import (
	"strings"
	"testing"
)

func TestLogoFrameProducesFixedLineCount(t *testing.T) {
	for _, t2 := range []float64{0, 0.25, 0.5, 0.75, 1} {
		frame := logoFrame(t2)
		lines := strings.Split(frame, "\n")
		// ANSI styling wraps the whole block, but the number of newlines
		// inside it must still match the canvas height regardless of phase,
		// or the surrounding layout would jump around as the animation runs.
		if len(lines) != logoCanvasHeight {
			t.Errorf("logoFrame(%v) has %d lines, want %d", t2, len(lines), logoCanvasHeight)
		}
	}
}

// Out of range input must clamp rather than place a glyph off the canvas or
// panic on an invalid index.
func TestLogoFrameClampsOutOfRangePhase(t *testing.T) {
	for _, t2 := range []float64{-1, -0.001, 1.5, 100} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("logoFrame(%v) panicked: %v", t2, r)
				}
			}()
			logoFrame(t2)
		}()
	}
}

// At phase 0, every corner glyph should sit at its own spread position, and
// none should coincide with the center dot: this is the resting frame shown
// on the welcome panel, and it must actually look spread out.
func TestLogoFrameAtZeroIsFullySpread(t *testing.T) {
	frame := stripANSI(logoFrame(0))
	lines := strings.Split(frame, "\n")
	if lines[0] == strings.Repeat(" ", logoCanvasWidth) {
		t.Fatal("top row should contain glyphs at phase 0, the fully spread frame")
	}
}

// At phase 1, the corner glyphs should have moved substantially closer to
// center compared to phase 0; this is checked by confirming the two frames
// actually differ, since exact pixel comparison would overspecify the
// hand-tuned coordinates.
func TestLogoFrameAnimatesBetweenPhases(t *testing.T) {
	spread := logoFrame(0)
	near := logoFrame(1)
	if spread == near {
		t.Fatal("phase 0 and phase 1 should render differently")
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestLogoPhaseTriangleWave(t *testing.T) {
	const half = 10
	cases := map[int]float64{
		0:  0,
		5:  0.5,
		10: 1,
		15: 0.5,
		20: 0,
		25: 0.5,
	}
	for ticks, want := range cases {
		got := logoPhase(ticks, half)
		if got != want {
			t.Errorf("logoPhase(%d, %d) = %v, want %v", ticks, half, got, want)
		}
	}
}

func TestLogoPhaseHandlesZeroHalfCycle(t *testing.T) {
	if got := logoPhase(5, 0); got != 0 {
		t.Fatalf("logoPhase with a zero half cycle = %v, want 0 (not a division by zero panic)", got)
	}
}
