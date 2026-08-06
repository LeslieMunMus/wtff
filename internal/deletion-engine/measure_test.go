package deletionengine

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
)

// A measurement that finishes in time reports its real answer.
func TestMeasureWithinReturnsAPromptResult(t *testing.T) {
	total, complete := measureWithin(time.Second, func() (int64, bool) {
		return 4096, true
	})
	if total != 4096 || !complete {
		t.Fatalf("got %d, %v, want 4096, true", total, complete)
	}
}

// The property that matters: a walk that never returns must not hold up the
// caller. This is the defect that produced a real multi hour hang, where the
// entry cap could not help because a stalled walk never reaches another entry
// to count.
func TestMeasureWithinAbandonsAStalledWalk(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	started := time.Now()
	total, complete := measureWithin(50*time.Millisecond, func() (int64, bool) {
		<-release
		return 999, true
	})
	elapsed := time.Since(started)

	if elapsed > time.Second {
		t.Fatalf("a stalled walk held the caller for %v", elapsed)
	}
	if complete {
		t.Fatal("an abandoned measurement must not claim its size is complete")
	}
	if total != 0 {
		t.Fatalf("an abandoned measurement reported %d bytes it never counted", total)
	}
}

// An abandoned walk that finishes later must be able to deliver and exit. An
// unbuffered channel would park it forever on a receiver that has gone,
// leaking a goroutine per stalled candidate.
//
// Goroutine exit is what gets checked, not the walk function returning. An
// earlier version of this test closed a channel from inside the walk with
// defer, which fires when the walk returns and says nothing about whether the
// send after it completed. It passed with the buffer removed.
func TestAbandonedWalkDoesNotLeakItsGoroutine(t *testing.T) {
	release := make(chan struct{})
	before := runtime.NumGoroutine()

	measureWithin(20*time.Millisecond, func() (int64, bool) {
		<-release
		return 1, true
	})
	close(release)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the abandoned walk never exited, it is parked on delivery")
}

// Concurrent measurements must not interfere, since Plan runs many in
// sequence and an abandoned one is still running during the next.
func TestMeasureWithinHandlesManyStalledWalks(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, complete := measureWithin(20*time.Millisecond, func() (int64, bool) {
				<-release
				return 1, true
			}); complete {
				t.Error("a stalled measurement claimed completeness")
			}
		}()
	}

	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("stalled measurements did not all give up")
	}
}

// walkSize still has to be correct, since the deadline only decides whether
// its answer is used.
func TestWalkSizeSumsATree(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, f := range []string{
		filepath.Join(root, "one"),
		filepath.Join(nested, "two"),
	} {
		if err := os.WriteFile(f, []byte("0123456789"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	total, complete := walkSize(root)
	if !complete {
		t.Fatal("an ordinary tree should measure completely")
	}
	if total != 20 {
		t.Fatalf("total = %d, want 20", total)
	}
}

func TestWalkSizeOnAnEmptyTree(t *testing.T) {
	total, complete := walkSize(t.TempDir())
	if !complete || total != 0 {
		t.Fatalf("an empty tree should measure completely as zero, got %d, %v",
			total, complete)
	}
}

// Planning must stay bounded even when measuring is slow, and an unmeasured
// entry must say so rather than reporting zero bytes as fact.
func TestPlanRemainsBoundedAndHonestAboutUnknownSizes(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "item")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	manifest, err := Plan([]Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		PlanOptions{Command: "test", Policy: AllowAll{}, Log: operationlog.Discard(), MeasureSizes: false})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if manifest.Entries[0].SizeKnown {
		t.Fatal("an unmeasured entry must not claim its size is known")
	}
	if !manifest.PartialSizing {
		t.Fatal("a manifest holding an unmeasured entry should report partial sizing")
	}
}
