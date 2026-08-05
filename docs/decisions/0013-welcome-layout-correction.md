# 0013: welcome layout corrected against the supplied mood board

Status: done.

## Context

The project manager reviewed the shipped home screen against a supplied reference,
`mood-board/claude-welcome-menu.png`, with the actual result saved side by side in
`current-render/`. The verdict was blunt and correct: the mark rendered as seven scattered
dots that did not read as a logo, the layout was a narrow column floating in a mostly empty
screen, and the multi-line animated canvas shown during operations was the same illegibility
problem in motion. A working lipgloss sketch of the intended layout was supplied alongside
the reference.

## What changed

The mark is now three hand-drawn frames of solid block glyphs on a compact seventeen by five
canvas, one two-lobed cluster per corner with notches facing the center dot, pulsing between
spread, mid, and near positions. Density is pinned by a test with a floor on solid glyphs per
frame, so the sparse-dots failure cannot silently return.

The home screen now follows the reference: a full-width header box with the program name
embedded in the top border, a two-column interior, welcome, mark, and home path on the left,
orientation text and the command list on the right, a note line under the box, and the prompt
anchored at the bottom of the screen above a key-hint footer. Dispatch logic is untouched;
only the view changed.

The activity indicator is now a single line, a small pulsing glyph in the brand color plus
label and elapsed timer, matching the reference's own activity line. The full mark appears
only on the welcome screen, static. The progress counter still awaits deletion engine
progress reporting, which remains the deferred Opus-tier work.

## The supplied code, verified before adapting

Structurally faithful to the reference, with three defects fixed during adaptation: hardcoded
widths that misalign the fake titled border whenever content width changes, replaced by
measuring the rendered box with display-width functions; byte-length arithmetic where display
width is required; and a static bottom padding standing in for real bottom anchoring, replaced
by height math against the view's actual dimensions.

## Process correction, recorded as a standing rule

This rework happened because the first version shipped without the project manager seeing a
preview. The rule going forward: any visual change gets one rendered snapshot for approval
before it is built out and committed. Cheaper in every way than rebuilding after rejection.

## Verification

Full suite green, vet and gofmt clean. One pseudo-terminal render at the reporting terminal's
own size, one hundred eighty by sixty, was inspected against the reference before commit:
titled border corners aligned, columns filled, prompt bottom-anchored.
