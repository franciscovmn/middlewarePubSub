package eventwire

import (
	"testing"
	"time"
)

func TestFairSchedulerPublisherLimitAndRoundRobin(t *testing.T) {
	s := NewFairScheduler()
	e := QueuedEvent{Payload: []byte{1}, Deadline: time.Now().Add(time.Second)}
	for i := 0; i < PublisherQueueLimit; i++ {
		if !s.Enqueue("a", e) {
			t.Fatalf("enqueue %d recusado", i)
		}
	}
	if s.Enqueue("a", e) {
		t.Fatal("excedeu limite por publicador")
	}
	if !s.Enqueue("b", e) {
		t.Fatal("segundo publicador recusado")
	}
	first, _, ok := s.Next(DispatchTurnBytes)
	if !ok || first != "a" {
		t.Fatalf("1o turno = %q", first)
	}
	second, _, ok := s.Next(DispatchTurnBytes)
	if !ok || second != "b" {
		t.Fatalf("2o turno = %q", second)
	}
}

func TestSchedulerExpiresQueuedEventAndEnforcesByteBudget(t *testing.T) {
	s := NewFairScheduler()
	e := QueuedEvent{Payload: make([]byte, MaxQueuedBytes-HeaderSize), Deadline: time.Now()}
	if !s.Enqueue("a", e) {
		t.Fatal("evento no limite de memória recusado")
	}
	if s.Enqueue("b", QueuedEvent{Payload: []byte{1}}) {
		t.Fatal("limite de bytes excedido")
	}
	_, got, ok := s.Next(DispatchTurnBytes)
	if !ok {
		t.Fatal("evento ausente")
	}
	if Remaining(got, time.Now()) != 0 {
		t.Fatal("evento expirado ainda válido")
	}
	if s.Bytes() != 0 || s.Len() != 0 {
		t.Fatalf("fila não esvaziou: %d bytes, %d eventos", s.Bytes(), s.Len())
	}
}

// TestNextLiveDescartaVencidosSemPerderAVez confere que eventos vencidos na
// frente da fila de um publicador não gastam a vez dele no rodízio.
func TestNextLiveDescartaVencidosSemPerderAVez(t *testing.T) {
	s := NewFairScheduler()
	now := time.Now()
	vencido := QueuedEvent{Payload: []byte{0}, Deadline: now.Add(-time.Millisecond)}
	for i := 0; i < 5; i++ {
		s.Enqueue("pesado", vencido)
	}
	s.Enqueue("pesado", QueuedEvent{Payload: []byte{1}, Deadline: now.Add(time.Second)})
	s.Enqueue("pesado", QueuedEvent{Payload: []byte{2}, Deadline: now.Add(time.Second)})
	s.Enqueue("leve", QueuedEvent{Payload: []byte{3}, Deadline: now.Add(time.Second)})

	id, e, expired, ok := s.NextLive(now)
	if !ok || id != "pesado" || e.Payload[0] != 1 || expired != 5 {
		t.Fatalf("1o turno = %q payload=%v vencidos=%d", id, e.Payload, expired)
	}
	if id, e, _, _ = s.NextLive(now); id != "leve" || e.Payload[0] != 3 {
		t.Fatalf("2o turno = %q payload=%v", id, e.Payload)
	}
	if id, e, _, _ = s.NextLive(now); id != "pesado" || e.Payload[0] != 2 {
		t.Fatalf("3o turno = %q payload=%v", id, e.Payload)
	}
	if _, _, _, ok = s.NextLive(now); ok || s.Len() != 0 || s.Bytes() != 0 {
		t.Fatalf("fila não esvaziou: %d eventos, %d bytes", s.Len(), s.Bytes())
	}

	s.Enqueue("so-vencidos", vencido)
	s.Enqueue("so-vencidos", vencido)
	if _, _, expired, ok = s.NextLive(now); ok || expired != 2 || s.Len() != 0 {
		t.Fatalf("fila só de vencidos: ok=%v vencidos=%d restantes=%d", ok, expired, s.Len())
	}
}

// TestRodizioNaoDeixaQuemVoltaFurarAFila confere que um publicador que
// esvazia a fila e publica de novo não é atendido antes de quem já esperava.
func TestRodizioNaoDeixaQuemVoltaFurarAFila(t *testing.T) {
	s := NewFairScheduler()
	e := QueuedEvent{Payload: []byte{1}, Deadline: time.Now().Add(time.Second)}
	for i := 0; i < 20; i++ {
		s.Enqueue("pesado", e)
	}
	s.Enqueue("leve", e)
	atendidos := map[string]int{}
	for i := 0; i < 20; i++ {
		id, _, ok := s.Next(DispatchTurnBytes)
		if !ok {
			t.Fatal("escalonador vazio")
		}
		atendidos[id]++
		if id == "leve" {
			// O leve publica de novo assim que esvazia a fila.
			s.Enqueue("leve", e)
		}
	}
	if atendidos["pesado"] < 9 {
		t.Fatalf("pesado atendido %d vezes em 20 turnos, leve %d", atendidos["pesado"], atendidos["leve"])
	}
}
