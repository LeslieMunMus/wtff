package terminalshell

import (
	"fmt"
	"sync/atomic"
)

// progressCounter carries a live "so far of total" figure from a worker
// goroutine to the render loop.
//
// The deletion engine calls its progress hook from whatever goroutine is doing
// the work, which is never the one Bubble Tea renders on. Sending a message per
// candidate would flood the event loop with updates nobody can read at that
// rate, and touching model state from the worker would be a data race.
//
// Two atomics avoid both. The worker stores; the spinner's own tick, which
// already fires several times a second, loads. Updates are naturally coalesced
// to the frame rate, and the engine stays free of any assumption about how the
// number reaches a screen.
type progressCounter struct {
	done  atomic.Int64
	total atomic.Int64
}

// report is the callback handed to the engine.
func (p *progressCounter) report(done, total int) {
	p.done.Store(int64(done))
	p.total.Store(int64(total))
}

// read returns the current figures.
func (p *progressCounter) read() (done, total int) {
	return int(p.done.Load()), int(p.total.Load())
}

// label renders the counter for the activity line, or empty before the worker
// has reported anything.
//
// Nothing is shown until a total is known. A bare "0 of 0" during the
// discovery phase, before the engine has any candidates to count, would read
// as a stalled operation at exactly the moment the operation is working
// hardest.
func (p *progressCounter) label() string {
	done, total := p.read()
	if total <= 0 {
		return ""
	}
	if done > total {
		done = total
	}
	return fmt.Sprintf("%d/%d", done, total)
}
