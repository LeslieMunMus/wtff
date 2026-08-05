package terminalshell

import (
	"strings"
	"testing"
)

// Every frame must be exactly the declared canvas size. The surrounding
// layout depends on the mark never changing dimensions between frames, or
// the welcome box and any screen showing the mark would jump as it pulses.
func TestLogoFramesAreUniformlySized(t *testing.T) {
	for i, frame := range logoFrames {
		if len(frame) != logoCanvasHeight {
			t.Errorf("frame %d has %d rows, want %d", i, len(frame), logoCanvasHeight)
		}
		for r, row := range frame {
			if got := len([]rune(row)); got != logoCanvasWidth {
				t.Errorf("frame %d row %d is %d runes wide, want %d", i, r, got, logoCanvasWidth)
			}
		}
	}
}

func TestLogoFrameProducesFixedLineCount(t *testing.T) {
	for step := range logoFrames {
		lines := strings.Split(logoFrame(step), "\n")
		if len(lines) != logoCanvasHeight {
			t.Errorf("logoFrame(%d) has %d lines, want %d", step, len(lines), logoCanvasHeight)
		}
	}
}

// Out of range input clamps rather than panicking on an invalid index.
func TestLogoFrameClampsOutOfRangeStep(t *testing.T) {
	for _, step := range []int{-5, -1, len(logoFrames), 100} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("logoFrame(%d) panicked: %v", step, r)
				}
			}()
			logoFrame(step)
		}()
	}
	if logoFrame(-1) != logoFrame(0) {
		t.Error("negative step should clamp to the first frame")
	}
	if logoFrame(100) != logoFrame(len(logoFrames)-1) {
		t.Error("oversized step should clamp to the last frame")
	}
}

// The frames must actually differ, or the pulse is a static image, and the
// mark must be dense: the first version of this file rendered seven single
// dots that did not read as a logo at all, so a floor on solid glyphs per
// frame pins the density that replaced them.
func TestLogoFramesAreDistinctAndDense(t *testing.T) {
	seen := map[string]bool{}
	for i, frame := range logoFrames {
		joined := strings.Join(frame[:], "\n")
		if seen[joined] {
			t.Errorf("frame %d duplicates an earlier frame", i)
		}
		seen[joined] = true

		solid := strings.Count(joined, "█") +
			strings.Count(joined, "▀") + strings.Count(joined, "▄")
		if solid < 8 {
			t.Errorf("frame %d has only %d solid block glyphs, too sparse to read as a mark", i, solid)
		}
	}
}

func TestLogoPhaseTriangleWalksFramesOutAndBack(t *testing.T) {
	const perStep = 2
	// With three frames the cycle over steps is 0,1,2,1 repeating; each step
	// lasts perStep ticks.
	cases := map[int]int{
		0: 0, 1: 0,
		2: 1, 3: 1,
		4: 2, 5: 2,
		6: 1, 7: 1,
		8: 0, 9: 0,
		10: 1,
	}
	for ticks, want := range cases {
		if got := logoPhase(ticks, perStep); got != want {
			t.Errorf("logoPhase(%d, %d) = %d, want %d", ticks, perStep, got, want)
		}
	}
}

func TestLogoPhaseHandlesZeroStepLength(t *testing.T) {
	if got := logoPhase(5, 0); got != 0 {
		t.Fatalf("logoPhase with a zero step length = %d, want 0, not a division panic", got)
	}
}
