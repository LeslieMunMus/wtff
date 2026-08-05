package terminalshell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// brandColor is wtff's fixed mark color, independent of the light or dark
// theme in use elsewhere. A logo that shifted color with the terminal's
// background would not be a mark at all, just another themed element.
const brandColor = lipgloss.Color("#0A0AAE")

// The mark is three hand-drawn frames, not interpolated points.
//
// The first version of this file plotted seven single dots on a 21 by 9
// canvas and moved them by interpolation. On screen that read as scattered
// periods, not a logo, which the project manager rejected on sight. The
// lesson kept here: a terminal mark needs density to read as a shape, so
// each frame is drawn by hand from solid block glyphs at a compact size,
// the same way other terminal marks that actually work are drawn, and the
// animation steps between whole frames instead of sliding individual cells.
//
// The geometry still follows the source image: one two-lobed cluster at the
// top with its notch facing down toward the center, two at the bottom with
// their notches facing up toward the center, and a center dot. The clusters
// pulse outward and inward, closing until they almost touch the center,
// matching the stated brief for the animation.
const (
	logoCanvasWidth  = 17
	logoCanvasHeight = 5
)

// logoFrames holds the pulse positions from fully spread, index 0, to
// almost touching the center, the last index. Every frame is exactly
// logoCanvasHeight lines of logoCanvasWidth characters, which the tests
// pin, since the surrounding layout depends on the mark's size never
// changing between frames.
var logoFrames = [3][logoCanvasHeight]string{
	{ // spread
		"       █▀█       ",
		"                 ",
		"        ●        ",
		"                 ",
		"  █▄█       █▄█  ",
	},
	{ // mid
		"                 ",
		"       █▀█       ",
		"        ●        ",
		"    █▄█   █▄█    ",
		"                 ",
	},
	{ // near, almost touching the center
		"                 ",
		"       █▀█       ",
		"        ●        ",
		"     █▄█ █▄█     ",
		"                 ",
	},
}

// logoFrame renders the mark at the given frame index, clamping anything
// out of range rather than panicking on an invalid index.
func logoFrame(step int) string {
	if step < 0 {
		step = 0
	}
	if step >= len(logoFrames) {
		step = len(logoFrames) - 1
	}
	lines := make([]string, logoCanvasHeight)
	for i, line := range logoFrames[step] {
		lines[i] = strings.TrimRight(line, " ")
	}
	return lipgloss.NewStyle().Foreground(brandColor).Bold(true).
		Render(strings.Join(lines, "\n"))
}

// logoPhase converts elapsed ticks into a frame index that walks up through
// the frames and back down, a triangle wave over whole frames, so the mark
// breathes continuously: spread, mid, near, mid, spread.
func logoPhase(elapsedTicks int, ticksPerStep int) int {
	if ticksPerStep <= 0 {
		return 0
	}
	steps := len(logoFrames) - 1
	if steps <= 0 {
		return 0
	}
	position := (elapsedTicks / ticksPerStep) % (steps * 2)
	if position <= steps {
		return position
	}
	return steps*2 - position
}
