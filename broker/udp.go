package broker

import (
	"errors"
	"net"
	"net/netip"
	"time"

	"github.com/franciscovmn/middlewarePubSub/internal/contract"
	"github.com/franciscovmn/middlewarePubSub/internal/eventwire"
	"github.com/franciscovmn/middlewarePubSub/topic"
)

type eventLimiter struct {
	events, bandwidth interface{ Allow(string, int) bool }
}

func newEventLimiter(events, bandwidth interface{ Allow(string, int) bool }) *eventLimiter {
	return &eventLimiter{events, bandwidth}
}
func (l *eventLimiter) AllowEvents(id string, n int) bool {
	return l.events.Allow(id, 1) && l.bandwidth.Allow(id, n)
}

func (b *Broker) serveUDP(conn *net.UDPConn) {
	defer b.wg.Done()
	buf := make([]byte, eventwire.HeaderSize+eventwire.MaxPayload+1)
	for {
		n, from, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			b.count("invalid_datagram")
			if !errors.Is(err, net.ErrClosed) {
				b.log.Warn("leitura UDP", "erro", err)
			}
			return
		}
		b.handleDatagram(conn, from, buf[:n])
	}
}

func (b *Broker) dispatchUDP(conn *net.UDPConn) {
	defer b.wg.Done()
	bucket := dispatchBucket{tokens: float64(b.cfg.DispatchBurst), updated: time.Now()}
	for {
		select {
		case <-b.stopDispatch:
			return
		case <-b.dispatch:
		case <-time.After(250 * time.Millisecond):
		}
		for {
			_, e, expired, ok := b.scheduler.NextLive(time.Now())
			for ; expired > 0; expired-- {
				b.count("expired")
			}
			if !ok {
				break
			}
			left := eventwire.Remaining(e, time.Now())
			left = left.Truncate(time.Millisecond)
			if left <= 0 {
				b.count("expired")
				continue
			}
			ref := topic.Ref{ID: e.Header.Topic, Generation: e.Header.Generation}
			_, ok = b.Contracts().Topics.Resolve(ref)
			if !ok {
				b.count("stale_topic")
				continue
			}
			e.Header.TTL = left
			packet, err := eventwire.Encode(e.Header, e.Payload)
			if err != nil {
				b.count("invalid_size")
				continue
			}
			for _, sub := range b.Contracts().Subscriptions.ForTopic(ref) {
				if !b.waitDispatchToken(&bucket) {
					return
				}
				left = eventwire.Remaining(e, time.Now()).Truncate(time.Millisecond)
				if left <= 0 {
					b.count("expired")
					break
				}
				e.Header.TTL = left
				packet, err = eventwire.Encode(e.Header, e.Payload)
				if err != nil {
					break
				}
				if _, err := conn.WriteToUDPAddrPort(packet, sub.UDPAddr); err != nil {
					b.count("delivery_error")
				}
			}
			if e.Header.Flags&eventwire.FlagPeerForwarded == 0 {
				link := b.Contracts().Link.Link()
				interest := b.Contracts().Interest.RemoteInterest(ref)
				if link.Alive && link.PeerUDP.IsValid() && interest.HasSubscribers && interest.Fresh(time.Now(), contract.InterestMaxAge) {
					left = forwardedTTL(eventwire.Remaining(e, time.Now()), link.RTT)
					left = left.Truncate(time.Millisecond)
					if left <= 0 {
						b.count("expired_forward")
						continue
					}
					e.Header.Flags |= eventwire.FlagPeerForwarded
					e.Header.TTL = left
					forward, err := eventwire.Encode(e.Header, e.Payload)
					if err == nil {
						if !b.waitDispatchToken(&bucket) {
							return
						}
						left = eventwire.Remaining(e, time.Now()).Truncate(time.Millisecond)
						left = forwardedTTL(left, link.RTT).Truncate(time.Millisecond)
						if left <= 0 {
							b.count("expired_forward")
							continue
						}
						e.Header.TTL = left
						forward, err = eventwire.Encode(e.Header, e.Payload)
						if err != nil {
							continue
						}
						if _, err = conn.WriteToUDPAddrPort(forward, link.PeerUDP); err != nil {
							b.count("peer_forward_error")
						} else {
							b.count("peer_forwarded")
						}
					}
				}
			}
			b.count("dispatched")
			b.gauge("queue_global", int64(b.scheduler.Len()))
			b.gauge("queue_bytes", int64(b.scheduler.Bytes()))
		}
	}
}

type dispatchBucket struct {
	tokens  float64
	updated time.Time
}

func (b *Broker) waitDispatchToken(bucket *dispatchBucket) bool {
	for {
		now := time.Now()
		bucket.tokens += now.Sub(bucket.updated).Seconds() * float64(b.cfg.DispatchRate)
		if max := float64(b.cfg.DispatchBurst); bucket.tokens > max {
			bucket.tokens = max
		}
		bucket.updated = now
		if bucket.tokens >= 1 {
			bucket.tokens--
			return true
		}
		wait := time.Duration((1 - bucket.tokens) / float64(b.cfg.DispatchRate) * float64(time.Second))
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		t := time.NewTimer(wait)
		select {
		case <-b.stopDispatch:
			t.Stop()
			return false
		case <-t.C:
		}
	}
}

func forwardedTTL(remaining, rtt time.Duration) time.Duration {
	d := remaining - rtt/2
	if d < 0 {
		return 0
	}
	return d
}

func (b *Broker) count(name string) {
	if s, ok := b.cfg.Stats.(interface{ Inc(string) }); ok {
		s.Inc(name)
	}
}
func (b *Broker) gauge(name string, value int64) {
	if s, ok := b.cfg.Stats.(interface{ SetGauge(string, int64) }); ok {
		s.SetGauge(name, value)
	}
}

func (b *Broker) handleDatagram(conn *net.UDPConn, from netip.AddrPort, packet []byte) {
	h, payload, err := eventwire.Decode(packet)
	if err != nil {
		b.count("invalid_datagram")
		b.log.Debug("datagrama UDP inválido", "erro", err)
		return
	}
	ref := topic.Ref{ID: h.Topic, Generation: h.Generation}
	if h.Flags&eventwire.FlagPeerForwarded != 0 {
		link := b.Contracts().Link.Link()
		if !link.Alive || !link.PeerUDP.IsValid() || netip.AddrPortFrom(from.Addr().Unmap(), from.Port()) != link.PeerUDP {
			b.count("invalid_peer_datagram")
			return
		}
		info, ok := b.Contracts().Topics.Resolve(ref)
		if !ok {
			b.count("topic_not_found")
			return
		}
		if !info.AcceptsTTL(h.TTL) {
			b.count("invalid_ttl")
			return
		}
		if !b.Contracts().Subscriptions.HasLocal(ref) {
			b.count("peer_without_local_interest")
			return
		}
		e := eventwire.QueuedEvent{Header: h, Payload: append([]byte(nil), payload...), Deadline: time.Now().Add(h.TTL)}
		if !b.scheduler.Enqueue("peer:"+link.PeerUDP.String(), e) {
			b.count("overloaded")
			return
		}
		b.count("peer_received")
		select {
		case b.dispatch <- struct{}{}:
		default:
		}
		return
	}
	sessionID := contract.SessionID(h.Session)
	sess, ok := b.Contracts().Sessions.Get(sessionID)
	if !ok || sess.UDPAddr != netip.AddrPortFrom(from.Addr().Unmap(), from.Port()) {
		return
	}
	result := contract.AdmissionResult{Session: sessionID, Topic: ref, Sequence: h.Sequence}
	if !b.cfg.EventLimiter.AllowEvents(sess.Identity, len(packet)) {
		result.Status = contract.AdmissionLimitExceeded
		b.count("rate_limited")
		b.Contracts().Results.Report(result)
		return
	}
	info, ok := b.Contracts().Topics.Resolve(ref)
	switch {
	case !ok:
		result.Status = contract.AdmissionTopicNotFound
		b.count("topic_not_found")
	case !info.AcceptsTTL(h.TTL):
		result.Status = contract.AdmissionInvalidTTL
		b.count("invalid_ttl")
	default:
		subs := b.Contracts().Subscriptions.ForTopic(ref)
		remote := b.Contracts().Interest.RemoteInterest(ref)
		fresh := remote.Fresh(time.Now(), contract.InterestMaxAge)
		link := b.Contracts().Link.Link()
		if len(subs) == 0 && !link.Alive {
			result.Status = contract.AdmissionUnavailable
			b.count("interest_unavailable")
		} else if len(subs) == 0 && fresh && !remote.HasSubscribers {
			result.Status = contract.AdmissionNoSubscribers
			b.count("no_subscribers")
		} else if len(subs) == 0 && (!fresh || !remote.HasSubscribers || !link.Alive || !link.PeerUDP.IsValid()) {
			result.Status = contract.AdmissionUnavailable
			b.count("interest_unavailable")
		} else {
			e := eventwire.QueuedEvent{Header: h, Payload: append([]byte(nil), payload...), Deadline: time.Now().Add(h.TTL)}
			if !b.scheduler.Enqueue(sess.Identity, e) {
				result.Status = contract.AdmissionOverloaded
				b.count("overloaded")
			} else {
				result.Status = contract.AdmissionAccepted
				b.count("accepted")
				b.gauge("queue_global", int64(b.scheduler.Len()))
				b.gauge("queue_bytes", int64(b.scheduler.Bytes()))
				select {
				case b.dispatch <- struct{}{}:
				default:
				}
			}
		}
	}
	b.Contracts().Results.Report(result)
}
