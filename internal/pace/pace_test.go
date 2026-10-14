package pace

import (
	"context"
	"testing"
	"time"
)

func TestPacerSpacing(t *testing.T) {
	p := NewPacer(60)
	if got := p.Spacing(); got != time.Second {
		t.Fatalf("Spacing = %v, want 1s", got)
	}
	if got := p.PerMinute(); got != 60 {
		t.Fatalf("PerMinute = %d, want 60", got)
	}
	p.SetPerMinute(0)
	if got := p.Spacing(); got != 0 {
		t.Fatalf("Spacing after disable = %v, want 0", got)
	}
}

func TestPacerWaitEnforcesGap(t *testing.T) {
	p := NewPacer(600) // 100ms spacing
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := p.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 190*time.Millisecond {
		t.Fatalf("three paced waits took %v, want at least ~200ms", elapsed)
	}
}

func TestPacerWaitHonorsContext(t *testing.T) {
	p := NewPacer(6) // 10s spacing
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(ctx); err == nil {
		t.Fatal("Wait ignored a cancelled context")
	}
}

func TestPacerDefer(t *testing.T) {
	p := NewPacer(6000) // 10ms spacing
	p.Defer(80 * time.Millisecond)
	start := time.Now()
	if err := p.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Fatalf("deferred wait took %v, want the deferral to apply", elapsed)
	}
}

func TestBackoffGrowsAndResets(t *testing.T) {
	b := NewBackoff(100*time.Millisecond, 2*time.Second)
	first := b.Delay(0, false)
	if first < 80*time.Millisecond || first > 130*time.Millisecond {
		t.Fatalf("first delay = %v, want ~100ms with jitter", first)
	}
	if b.Attempt() != 1 {
		t.Fatalf("attempt = %d, want 1", b.Attempt())
	}
	third := b.Delay(0, false)
	fourth := b.Delay(0, false)
	if !(third > first && fourth > third) {
		t.Fatalf("delays did not grow: %v, %v, %v", first, third, fourth)
	}
	for i := 0; i < 20; i++ {
		if d := b.Delay(0, false); d > 2*time.Second {
			t.Fatalf("delay %v exceeded the cap", d)
		}
	}
	b.Reset()
	if b.Attempt() != 0 {
		t.Fatalf("attempt after reset = %d, want 0", b.Attempt())
	}
}

func TestBackoffPrefersRetryAfter(t *testing.T) {
	b := NewBackoff(100*time.Millisecond, 10*time.Second)
	if got := b.Delay(3*time.Second, true); got != 3*time.Second {
		t.Fatalf("delay = %v, want the 3s Retry-After", got)
	}
	if got := b.Delay(0, true); got != 0 {
		t.Fatalf("delay = %v, want an immediate retry when the server says so", got)
	}
	if got := b.Delay(30*time.Second, true); got != 10*time.Second {
		t.Fatalf("delay = %v, want it capped at Max", got)
	}
}
