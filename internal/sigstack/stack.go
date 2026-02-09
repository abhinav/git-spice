// Package sigstack provides a stack-based signal handler
// that allows multiple components to register handlers
// for the same signal without clobbering each other.
package sigstack

import (
	"os"
	"os/signal"
	"slices"
	"sync"
)

// Signal is an alias for [os.Signal].
type Signal = os.Signal

// Stack manages a stack of signal handlers per signal.
// When a signal fires, only the topmost channel receives it.
//
// The zero value is ready to use.
type Stack struct {
	mu         sync.Mutex
	states     map[Signal]*sigState
	sigsByRecv map[chan<- Signal][]Signal
}

// sigState holds per-signal state:
// the OS channel, the dispatch goroutine's done signal,
// and the stack of registered channels.
type sigState struct {
	ossig chan os.Signal  // receives from os/signal
	done  chan struct{}   // stops dispatch goroutine
	recvs []chan<- Signal // stack of registered channels
}

// Notify registers ch to receive the given signals,
// adding it to the top of the handler stack for each signal.
//
// Like [signal.Notify], signals are sent non-blocking,
// so the caller should use a buffered channel.
func (s *Stack) Notify(ch chan<- Signal, sigs ...Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.states == nil {
		s.states = make(map[Signal]*sigState)
	}
	if s.sigsByRecv == nil {
		s.sigsByRecv = make(map[chan<- Signal][]Signal)
	}

	for _, sig := range sigs {
		state, ok := s.states[sig]
		if !ok {
			state = &sigState{
				ossig: make(chan os.Signal, 1),
				done:  make(chan struct{}),
			}
			s.states[sig] = state
			signal.Notify(state.ossig, sig)
			go s.dispatch(sig, state)
		}
		state.recvs = append(state.recvs, ch)
	}

	s.sigsByRecv[ch] = append(s.sigsByRecv[ch], sigs...)
}

// Stop unregisters ch from all signals
// and removes it from the handler stacks.
//
// It is safe to call Stop multiple times.
func (s *Stack) Stop(ch chan<- Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sigs, ok := s.sigsByRecv[ch]
	if !ok {
		return
	}

	for _, sig := range sigs {
		state := s.states[sig]
		state.recvs = slices.DeleteFunc(state.recvs, func(c chan<- Signal) bool {
			return c == ch
		})

		// If the stack is empty,
		// tear down the OS handler and dispatch goroutine.
		if len(state.recvs) == 0 {
			signal.Stop(state.ossig)
			close(state.done)
			delete(s.states, sig)
		}
	}

	delete(s.sigsByRecv, ch)
}

// dispatch reads from the OS signal channel
// and sends to the topmost registered channel.
func (s *Stack) dispatch(sig Signal, state *sigState) {
	for {
		select {
		case <-state.ossig:
			s.mu.Lock()
			var top chan<- Signal
			if n := len(state.recvs); n > 0 {
				top = state.recvs[n-1]
			}
			s.mu.Unlock()

			if top != nil {
				// Non-blocking send, like os/signal.
				select {
				case top <- sig:
				default:
				}
			}

		case <-state.done:
			return
		}
	}
}
