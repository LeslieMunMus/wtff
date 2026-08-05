package terminalshell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// brandColor is wtff's fixed mark color, independent of the light or dark
// theme in use elsewhere. A logo that shifted color with the terminal's
// background would not be a mark at all, just another themed element; it is
// meant to look the same regardless of what it is drawn against.
const brandColor = lipgloss.Color("#0A0AAE")

// logoCanvasWidth and logoCanvasHeight define the fixed character grid the
// animated mark is drawn on. Terminal cells are roughly twice as tall as
// they are wide, so a canvas meant to read as a triangle needs noticeably
// more columns than rows to avoid looking compressed.
const (
	logoCanvasWidth  = 21
	logoCanvasHeight = 9
)

// logoNode is one of the three corner clusters or the center dot. Each
// corner is represented as a pair of glyphs standing in for the pinched,
// two lobed shape in the source mark; a terminal's fixed character grid
// cannot reproduce the mark's soft, continuous curve, so this is a
// recognizable approximation of it, not a reproduction.
type logoNode struct {
	// spreadRow, spreadCol is the glyph's position at full extension, the
	// resting frame the animation returns to.
	spreadRow, spreadCol float64
	// nearRow, nearCol is its position at the animation's inward extreme,
	// close to the center dot without touching it. The two ends of a pair
	// converge as they approach center, matching how the source mark's
	// lobes narrow toward its middle.
	nearRow, nearCol float64
}

// logoNodes lists both glyphs of each of the three corner clusters. Center
// row 4, center col 10 is the canvas midpoint; positions are hand placed to
// read as a triangle at this specific character grid size rather than
// derived from exact geometry, since a low resolution grid does not reward
// that precision.
var logoNodes = [3][2]logoNode{
	{ // top
		{spreadRow: 0, spreadCol: 8, nearRow: 3, nearCol: 9},
		{spreadRow: 0, spreadCol: 12, nearRow: 3, nearCol: 11},
	},
	{ // bottom left
		{spreadRow: 8, spreadCol: 3, nearRow: 5, nearCol: 8},
		{spreadRow: 7, spreadCol: 1, nearRow: 6, nearCol: 7},
	},
	{ // bottom right
		{spreadRow: 8, spreadCol: 17, nearRow: 5, nearCol: 12},
		{spreadRow: 7, spreadCol: 19, nearRow: 6, nearCol: 13},
	},
}

const (
	logoCenterRow = 4
	logoCenterCol = 10
)

// logoFrame renders the mark at animation phase t, where 0 is fully spread
// and 1 is closest to center. Callers drive t through a triangle wave, out
// then back, rather than calling this with a monotonically increasing value,
// since the mark is meant to breathe continuously, not extend once and stop.
func logoFrame(t float64) string {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}

	grid := make([][]rune, logoCanvasHeight)
	for r := range grid {
		grid[r] = make([]rune, logoCanvasWidth)
		for c := range grid[r] {
			grid[r][c] = ' '
		}
	}

	place := func(row, col float64, glyph rune) {
		r := int(row + 0.5)
		c := int(col + 0.5)
		if r < 0 || r >= logoCanvasHeight || c < 0 || c >= logoCanvasWidth {
			return
		}
		grid[r][c] = glyph
	}

	for _, pair := range logoNodes {
		for _, node := range pair {
			row := node.spreadRow + (node.nearRow-node.spreadRow)*t
			col := node.spreadCol + (node.nearCol-node.spreadCol)*t
			place(row, col, '●')
		}
	}
	place(logoCenterRow, logoCenterCol, '●')

	lines := make([]string, logoCanvasHeight)
	for r, row := range grid {
		lines[r] = strings.TrimRight(string(row), " ")
	}
	return lipgloss.NewStyle().Foreground(brandColor).Bold(true).
		Render(strings.Join(lines, "\n"))
}

// logoPhase converts an elapsed duration into the 0 to 1 to 0 triangle wave
// logoFrame expects, so the mark spreads out and draws back in on a
// continuous loop rather than snapping between two states.
func logoPhase(elapsedTicks int, ticksPerHalfCycle int) float64 {
	if ticksPerHalfCycle <= 0 {
		return 0
	}
	position := elapsedTicks % (ticksPerHalfCycle * 2)
	if position <= ticksPerHalfCycle {
		return float64(position) / float64(ticksPerHalfCycle)
	}
	return float64(ticksPerHalfCycle*2-position) / float64(ticksPerHalfCycle)
}
