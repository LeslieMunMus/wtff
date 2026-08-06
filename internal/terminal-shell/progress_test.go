package terminalshell

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// Nothing is shown before the work reports a total. A bare "0/0" during
// discovery, before the engine has candidates to count, reads as a stall at
// exactly the moment the operation is working hardest.
func TestProgressIsSilentUntilATotalIsKnown(t *testing.T) {
	var counter progressCounter
	if label := counter.label(); label != "" {
		t.Fatalf("expected no label before any report, got %q", label)
	}

	counter.report(0, 86)
	if label := counter.label(); label != "0/86" {
		t.Fatalf("label = %q, want 0/86", label)
	}
}

func TestProgressTracksTheWork(t *testing.T) {
	var counter progressCounter
	counter.report(47, 86)

	done, total := counter.read()
	if done != 47 || total != 86 {
		t.Fatalf("read %d of %d, want 47 of 86", done, total)
	}
	if label := counter.label(); label != "47/86" {
		t.Fatalf("label = %q, want 47/86", label)
	}
}

// The engine reports the index before handling each item, so the last report
// is one short of the total. Showing "87/86" would be worse than showing the
// count standing still for a moment.
func TestProgressNeverExceedsItsTotal(t *testing.T) {
	var counter progressCounter
	counter.report(120, 86)
	if label := counter.label(); label != "86/86" {
		t.Fatalf("label = %q, want it clamped to 86/86", label)
	}
}

// The whole point of the type: the engine reports from a worker goroutine
// while the render loop reads. Anything but atomics here is a data race, which
// the race detector will fail this test for.
func TestProgressIsSafeAcrossGoroutines(t *testing.T) {
	var counter progressCounter
	var wg sync.WaitGroup

	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			counter.report(i, 2000)
		}
		close(stop)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = counter.label()
			}
		}
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent reporting and reading did not finish")
	}
}

// The activity line shows the counter alongside the timer once work reports.
func TestActivityLineShowsTheCounter(t *testing.T) {
	counter := &progressCounter{}
	indicator := newActivityIndicator("Scanning").withProgress(counter)

	// Before any report, the line is the label and the timer only.
	if strings.Contains(indicator.view(brandTheme), "/") {
		t.Fatal("no counter should appear before the work reports one")
	}

	counter.report(12, 40)
	view := indicator.view(brandTheme)
	if !strings.Contains(view, "12/40") {
		t.Fatalf("the activity line should show the counter, got %q", view)
	}
	if !strings.Contains(view, "Scanning") {
		t.Fatalf("the activity line should keep its label, got %q", view)
	}
	if strings.Contains(view, "\n") {
		t.Fatal("the activity line must stay one line")
	}
}

// Work that cannot report a total still gets a working indicator.
func TestActivityLineWithoutACounter(t *testing.T) {
	view := newActivityIndicator("Loading").view(brandTheme)
	if !strings.Contains(view, "Loading") {
		t.Fatalf("expected the label, got %q", view)
	}
	if strings.Contains(view, "/") {
		t.Fatalf("expected no counter, got %q", view)
	}
}
