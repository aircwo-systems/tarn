package interpreter

import "time"

// Clock abstracts the wall clock so Wait-state sleeps and execution timeouts
// are testable without real sleeps.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// After returns a channel that receives after duration d, analogous to
	// time.After. The interpreter never calls time.After directly.
	After(d time.Duration) <-chan time.Time
}

// SystemClock is the production Clock backed by the real time package.
var SystemClock Clock = systemClock{}

type systemClock struct{}

func (systemClock) Now() time.Time                         { return time.Now() }
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
