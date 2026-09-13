// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import "sync/atomic"

// Password verification is deliberately expensive: DefaultHashCost is RFC 9106's 64 MiB, three
// passes, four lanes, and an operator may configure up to the 256 MiB checkCost permits. That
// cost is the point when it bounds an attacker's guessing rate, and a liability when anyone
// unauthenticated can start an unbounded number of them at once - on the Pi-class hosts this
// project targets, a few dozen concurrent logins is the whole machine's memory. Lockout does not
// help: it is per username and source, so distinct values sail past it, and before this change
// even a locked-out request ran the hash in full before returning 429.
//
// So admission is bounded here rather than punitively. A bounded pool caps how much verification
// can be in flight, and a bounded queue caps how many callers may wait for it; past that the
// request is refused immediately, having allocated nothing. What it deliberately does NOT do is
// lock out a source address: internal/auth.ClientIP reports the direct peer, and behind the
// reverse proxy this project expects that is one address for every user, so an address-keyed
// lockout would be a way to log the whole household out rather than a defence.
const (
	maxConcurrentVerifications = 2
	maxQueuedVerifications     = 8
)

// verifier admits at most maxConcurrentVerifications password hashes at once, with at most
// maxQueuedVerifications callers waiting.
type verifier struct {
	slots   chan struct{}
	waiting atomic.Int64
	queue   int64
	// admitted counts verifications actually started. Every password hash in this package goes
	// through verify, so this is the one number that says whether a request cost a hash - which
	// is exactly what the admission tests need to assert and what no timing measurement could
	// establish reliably against a cheap test cost.
	admitted atomic.Int64
}

func newVerifier(concurrency, queue int) *verifier {
	if concurrency < 1 {
		concurrency = 1
	}
	if queue < 0 {
		queue = 0
	}
	return &verifier{slots: make(chan struct{}, concurrency), queue: int64(queue)}
}

// verify runs fn under the pool, or returns ErrBusy without running it when too many callers are
// already waiting. The refusal happens before any memory is allocated for a hash, which is the
// entire point: a request that is going to be refused must not first cost what it is being
// refused for.
func (v *verifier) verify(fn func() bool) (bool, error) {
	if v.waiting.Add(1) > v.queue+int64(cap(v.slots)) {
		v.waiting.Add(-1)
		return false, ErrBusy
	}
	defer v.waiting.Add(-1)
	v.slots <- struct{}{}
	defer func() { <-v.slots }()
	v.admitted.Add(1)
	return fn(), nil
}
