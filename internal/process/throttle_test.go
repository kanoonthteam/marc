package process

import (
	"context"
	"testing"
	"time"
)

func TestThrottleDenoise(t *testing.T) {
	t.Run("disabled is a no-op", func(t *testing.T) {
		d := &daemon{} // denoiseInterval == 0
		start := time.Now()
		if err := d.throttleDenoise(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if waited := time.Since(start); waited > 10*time.Millisecond {
			t.Errorf("disabled gate waited %v, want ~0", waited)
		}
	})

	t.Run("spaces consecutive calls", func(t *testing.T) {
		d := &daemon{denoiseInterval: 40 * time.Millisecond}
		// first call: lastDenoise is zero, so no wait
		if err := d.throttleDenoise(context.Background()); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if err := d.throttleDenoise(context.Background()); err != nil {
			t.Fatal(err)
		}
		if waited := time.Since(start); waited < 30*time.Millisecond {
			t.Errorf("second call waited %v, want >=~40ms spacing", waited)
		}
	})

	t.Run("ctx cancellation aborts the wait", func(t *testing.T) {
		d := &daemon{denoiseInterval: time.Hour}
		if err := d.throttleDenoise(context.Background()); err != nil { // sets lastDenoise
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := d.throttleDenoise(ctx); err == nil {
			t.Error("expected ctx error when cancelled during the wait")
		}
	})
}
