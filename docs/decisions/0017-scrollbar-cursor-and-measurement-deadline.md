# 0017: scrollbar, cursor feedback, and a real bound on measuring

Status: done.

## Two things that were silently inert

The transcript could not be scrolled with the wheel, and the cursor never
blinked. Both had the same cause and neither produced an error.

`Update` switched on message type, handled window size, flow messages, and key
messages, and sent everything else to a running block. Mouse messages had no
case, so with no block running the wheel reached nothing at all. Cursor blink
messages are not key messages either, so once any flow had run they reached
nothing either and the cursor stopped animating for good.

The fix routes mouse messages to the viewport in their own case, and offers
every unhandled message to the prompt as well as to any running block. Arrow
keys now scroll too, but only when the palette is closed and no block is live,
since both of those claim the arrows for their own cursor.

Focus now follows the live block: the prompt blurs when a flow starts and takes
the cursor back when it ends, so the blink is always at the place typing
actually goes.

## The scrollbar

The viewport component ships without one, which is why a transcript that
scrolled perfectly well read as stuck: nothing on screen said there was
anything above the fold. The bar is one column, the thumb in the main color
over a track in the highlight color, as specified.

Its column comes out of the viewport's width rather than being added beside it,
so the two together are exactly the terminal width. Adding it beside would push
every line one column over and wrap the whole transcript, so a test asserts no
rendered line exceeds the terminal width.

The column is reserved whether or not the bar is visible. Showing it only on
overflow would reflow the entire transcript the moment history passed one
screen.

## Palette separation at the prompt

The suggestion is now `#D6D6D6`, distinct from typed text, which keeps its own
color. They occupy the same cells, and with one color an empty prompt and a
real command read at the same weight. The echoed command in the transcript
moved from the body color to the main color: it is a heading for everything
indented beneath it, and in the reading color it rendered as near black against
its own output.

## The measurement hang, actually fixed this time

The earlier work made the spinner honest about a slow scan. It did not make the
scan bounded, and the comment above the entry cap claimed a property the code
did not have: "must never be the reason a plan hangs," when the cap counts
entries and nothing counted time.

That distinction is the whole bug. A walk stalled inside a single filesystem
call never reaches another entry to be counted, so the cap never trips. A
network mount that stops answering, an unresponsive user space filesystem, or a
disk spinning up all stall exactly that way, and that is what produced a real
multi hour hang.

A deadline checked between entries would not help either, for the same reason.
So the walk runs on its own goroutine and is abandoned if it does not finish in
three seconds, reporting the size as unknown rather than never reporting at
all. Abandoning rather than cancelling is deliberate: a stalled walk is blocked
inside a call that takes no cancellation, so the only available action is to
stop waiting. The abandoned goroutine owns its own accumulator, so nothing
reads a value another goroutine is still writing, and its channel is buffered
so it can deliver and exit instead of parking forever on a receiver that has
gone.

A per candidate deadline still allows a plan over many candidates to take their
sum, so the run also carries a twenty second total measuring budget. Past it,
remaining candidates are planned without sizes, and say so.

## Red team

**A test that passed for the wrong reason.** The goroutine leak test closed a
channel from inside the walk function with defer. That fires when the walk
returns, which says nothing about whether the send after it completed, and it
passed with the channel's buffer removed. Rewritten to check goroutine exit.

**A test whose name did not match its body.** A case named for the entry cap
measured an empty directory and asserted nothing about the cap. Renamed to what
it actually checks rather than left as a false claim of coverage.

Every fix here was then verified by mutation: the mouse case, the blink
routing, the reserved scrollbar column, the measurement deadline, and the
channel buffer were each reverted in turn and the matching test confirmed to
fail. Reverting the deadline hangs the suite until its own timeout, which is
the original defect reproduced exactly.

## Verification

Full suite green under the race detector. The compiled binary was driven
through a pseudo terminal against an isolated home: the thumb renders in
`10;10;174` and the track in `225;225;253`, the brand main and highlight
colors; the placeholder renders in `214;214;214` and typed text does not; the
echoed command renders in the main color and not the body color; arrow keys
scroll the transcript; and with no input at all the terminal received three
distinct redraws over three and a half seconds, which is the cursor animating.

## Known limitations

The scrollbar is not draggable, since the shell reads wheel and key events but
does not hit test clicks against the bar. The measurement deadlines are
constants rather than settings.
