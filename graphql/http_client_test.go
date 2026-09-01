package graphql

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHTTPClientRateLimitDisabled(t *testing.T) {
	cfg := &graphqlProviderConfig{RateLimitPerSecond: 0, RateLimitBurst: 1}
	client := newHTTPClient(cfg)

	if _, ok := client.Transport.(*rateLimitedTransport); ok {
		t.Fatal("expected no rateLimitedTransport when rate_limit_per_second is 0")
	}
}

func TestNewHTTPClientRateLimitEnabled(t *testing.T) {
	cfg := &graphqlProviderConfig{RateLimitPerSecond: 5, RateLimitBurst: 1}
	client := newHTTPClient(cfg)

	if _, ok := client.Transport.(*rateLimitedTransport); !ok {
		t.Fatalf("expected rateLimitedTransport when rate_limit_per_second is non-zero, got %T", client.Transport)
	}
}

func TestRateLimitedTransportPacesRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	const ratePerSecond = 20.0
	const requests = 5

	cfg := &graphqlProviderConfig{RateLimitPerSecond: ratePerSecond, RateLimitBurst: 1}
	client := newHTTPClient(cfg)

	start := time.Now()
	for i := 0; i < requests; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		resp.Body.Close()
	}
	elapsed := time.Since(start)

	// With burst 1, the first request is free and each of the remaining
	// (requests-1) waits ~1/rate. Allow slack below the theoretical minimum.
	minExpected := time.Duration(float64(requests-1)/ratePerSecond*float64(time.Second)) * 9 / 10
	if elapsed < minExpected {
		t.Fatalf("expected rate limiting to take at least %s, took %s", minExpected, elapsed)
	}
}
