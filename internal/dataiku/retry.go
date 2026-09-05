package dataiku

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultMaxRetries = 3
	baseBackoff       = 500 * time.Millisecond
	maxBackoff        = 30 * time.Second
)

// shouldRetry decides whether a failed attempt is worth repeating.
//
// Idempotent methods are safe to repeat whatever went wrong. POST is not: a 5xx
// or a dropped connection can mean the object was created and only the response
// was lost, and repeating that would create a second project or user. So POST
// is only retried on 429, where the server has explicitly said it did not
// process the request.
func shouldRetry(method string, resp *http.Response, err error) bool {
	idempotent := method == http.MethodGet ||
		method == http.MethodPut ||
		method == http.MethodDelete ||
		method == http.MethodHead

	if err != nil {
		// A transport error with no response: the request may or may not have
		// been processed, so only repeat it when repeating is harmless.
		return idempotent
	}

	if resp == nil {
		return false
	}

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return idempotent
	default:
		return false
	}
}

// retryAfter honours a Retry-After header, which DSS sends behind some
// gateways, falling back to exponential backoff with jitter.
func retryAfter(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if header := resp.Header.Get("Retry-After"); header != "" {
			if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
				return capBackoff(time.Duration(seconds) * time.Second)
			}
			if when, err := http.ParseTime(header); err == nil {
				if wait := time.Until(when); wait > 0 {
					return capBackoff(wait)
				}
			}
		}
	}

	backoff := float64(baseBackoff) * math.Pow(2, float64(attempt))
	// Full jitter, so that several resources applying in parallel do not all
	// come back at the same moment.
	jittered := time.Duration(rand.Int63n(int64(backoff) + 1))
	if jittered < baseBackoff {
		jittered = baseBackoff
	}
	return capBackoff(jittered)
}

func capBackoff(d time.Duration) time.Duration {
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// sleepWithContext waits for d, or returns early if the context is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// isContextError reports whether err came from the caller giving up, in which
// case retrying is pointless.
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
