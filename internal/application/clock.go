package application

import "time"

// Clock returns the current time. Services take one so tests can control
// freshness and timeouts without sleeping.
type Clock func() time.Time

func now() time.Time { return time.Now() }

func orNow(c Clock) Clock {
	if c == nil {
		return now
	}
	return c
}
