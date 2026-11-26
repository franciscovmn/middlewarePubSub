package broker

import (
	"testing"
	"time"
)

func TestForwardedTTLSubtractsHalfRTT(t *testing.T) {
	if got := forwardedTTL(500*time.Millisecond, 80*time.Millisecond); got != 460*time.Millisecond {
		t.Fatalf("TTL após RTT/2=%v", got)
	}
	if got := forwardedTTL(20*time.Millisecond, 80*time.Millisecond); got != 0 {
		t.Fatalf("TTL deveria expirar, obtido %v", got)
	}
}

// TestDispatchBucketAllowsBurstThenPaces confere que a rajada sai sem
// espera e que, esgotada, o envio segue a taxa configurada.
func TestDispatchBucketAllowsBurstThenPaces(t *testing.T) {
	b := &Broker{cfg: Config{DispatchRate: 200, DispatchBurst: 10}, stopDispatch: make(chan struct{})}
	bucket := dispatchBucket{tokens: 10, updated: time.Now()}

	start := time.Now()
	for i := 0; i < 10; i++ {
		if !b.waitDispatchToken(&bucket) {
			t.Fatal("rajada recusada")
		}
	}
	if d := time.Since(start); d > 20*time.Millisecond {
		t.Fatalf("rajada de 10 levou %v", d)
	}

	start = time.Now()
	for i := 0; i < 20; i++ {
		b.waitDispatchToken(&bucket)
	}
	if d := time.Since(start); d < 80*time.Millisecond || d > 300*time.Millisecond {
		t.Fatalf("20 envios a 200/s levaram %v, esperado cerca de 100 ms", d)
	}

	close(b.stopDispatch)
	bucket.tokens = 0
	if b.waitDispatchToken(&bucket) {
		t.Fatal("espera deveria terminar com o broker encerrado")
	}
}
