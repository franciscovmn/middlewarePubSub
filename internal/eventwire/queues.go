package eventwire

import (
	"sync"
	"time"

	"github.com/franciscovmn/middlewarePubSub/topic"
)

const (
	PublisherQueueLimit = 1024
	GlobalQueueLimit    = 65536
	DispatchTurnBytes   = 4096
	MaxQueuedBytes      = 64 << 20
)

type BoundedQueue struct {
	mu    sync.Mutex
	items []QueuedEvent
	cap   int
	state bool
}

func NewQueue(info topic.Info) *BoundedQueue {
	return &BoundedQueue{cap: info.Policy.QueueCapacity, state: info.Kind == topic.KindState}
}
func NewBoundedQueue(capacity int) *BoundedQueue {
	if capacity < 1 {
		capacity = 1
	}
	return &BoundedQueue{cap: capacity}
}
func (q *BoundedQueue) Push(e QueuedEvent) (int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	d := 0
	if len(q.items) >= q.cap {
		if q.state {
			q.items = q.items[:0]
			d = 1
		} else {
			q.items = q.items[1:]
			d = 1
		}
	}
	q.items = append(q.items, e)
	return d, true
}
func (q *BoundedQueue) Pop() (QueuedEvent, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return QueuedEvent{}, false
	}
	e := q.items[0]
	q.items[0] = QueuedEvent{}
	q.items = q.items[1:]
	return e, true
}
func (q *BoundedQueue) Len() int { q.mu.Lock(); defer q.mu.Unlock(); return len(q.items) }

// FairScheduler aplica limites por publicador e globais e agenda publicadores
// em rodízio, até um evento por turno (no máximo 4 KiB por orçamento).
type FairScheduler struct {
	mu     sync.Mutex
	queues map[string]*BoundedQueue
	order  []string
	cursor int
	total  int
	bytes  int
}

func NewFairScheduler() *FairScheduler { return &FairScheduler{queues: map[string]*BoundedQueue{}} }
func (s *FairScheduler) Enqueue(id string, e QueuedEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.queues[id]
	if q == nil {
		q = NewBoundedQueue(PublisherQueueLimit)
		s.queues[id] = q
	}
	size := len(e.Payload) + HeaderSize
	if s.total >= GlobalQueueLimit || q.Len() >= PublisherQueueLimit || s.bytes+size > MaxQueuedBytes {
		return false
	}
	if q.Len() == 0 {
		// Publicador que volta ao rodízio entra logo antes do cursor, no fim
		// da volta em curso. No fim da lista ele poderia cair sob o cursor e
		// ser atendido antes de quem já esperava.
		if s.cursor >= len(s.order) {
			s.cursor = 0
		}
		s.order = append(s.order, "")
		copy(s.order[s.cursor+1:], s.order[s.cursor:])
		s.order[s.cursor] = id
		s.cursor++
	}
	q.Push(e)
	s.total++
	s.bytes += size
	return true
}
func (s *FairScheduler) Next(budget int) (string, QueuedEvent, bool) {
	id, e, _, ok := s.next(time.Time{})
	return id, e, ok
}

// NextLive é Next descartando os eventos já vencidos em now. O descarte não
// gasta a vez do publicador: com a fila dele atrasada além da validade, o
// turno segue para o primeiro evento ainda válido da mesma fila, em vez de
// passar ao próximo publicador. expired conta os descartados.
func (s *FairScheduler) NextLive(now time.Time) (id string, e QueuedEvent, expired int, ok bool) {
	return s.next(now)
}

func (s *FairScheduler) next(now time.Time) (string, QueuedEvent, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	expired := 0
	for len(s.order) > 0 {
		if s.cursor >= len(s.order) {
			s.cursor = 0
		}
		id := s.order[s.cursor]
		q := s.queues[id]
		e, ok := q.Pop()
		if ok {
			s.total--
			s.bytes -= len(e.Payload) + HeaderSize
		}
		if q.Len() == 0 {
			delete(s.queues, id)
			s.order = append(s.order[:s.cursor], s.order[s.cursor+1:]...)
		} else if ok && (now.IsZero() || Remaining(e, now) > 0) {
			s.cursor++
		}
		if !ok {
			continue
		}
		if !now.IsZero() && Remaining(e, now) <= 0 {
			expired++
			continue
		}
		return id, e, expired, true
	}
	return "", QueuedEvent{}, expired, false
}
func (s *FairScheduler) Len() int   { s.mu.Lock(); defer s.mu.Unlock(); return s.total }
func (s *FairScheduler) Bytes() int { s.mu.Lock(); defer s.mu.Unlock(); return s.bytes }

func Remaining(e QueuedEvent, now time.Time) time.Duration {
	d := e.Deadline.Sub(now)
	if d < 0 {
		return 0
	}
	return d
}
