package main

import (
	"log"
	"net/http"
	"os"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/sullivtr/terraform-provider-graphql/gql-server/graph"
	"github.com/sullivtr/terraform-provider-graphql/gql-server/graph/generated"
	"golang.org/x/time/rate"
)

const defaultPort = "8080"

// rateLimitHeader opts a request into server-side rate limiting. Only the
// rate limiting e2e fixture sends it, so the other fixtures are unaffected.
const rateLimitHeader = "x-e2e-rate-limit"

// serverRatePerSecond is the request rate the server enforces for opted-in
// requests, using the same token-bucket limiter (golang.org/x/time/rate) as
// the provider. A client paced below this rate is always allowed; an
// unthrottled client bursts past it and gets rejected.
const serverRatePerSecond = 2.5

// retryAfterSeconds is advertised in the Retry-After header of a 429 response,
// mirroring the delay-seconds form a real rate-limited API returns.
const retryAfterSeconds = "1"

// serverLimiter enforces serverRatePerSecond with a burst of 1. Allow only
// consumes a token when it returns true, so rejected requests do not advance
// the bucket and a burst keeps failing until it slows down.
var serverLimiter = rate.NewLimiter(rate.Limit(serverRatePerSecond), 1)

// rateLimitMiddleware rejects opted-in requests that exceed the limiter with a
// realistic 429: a Retry-After header and a plain-text body rather than a
// GraphQL envelope. The current provider fails to decode that body and surfaces
// the apply error; a future change can special-case the 429 status and honor
// the Retry-After header before decoding.
func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(rateLimitHeader) != "" && !serverLimiter.Allow() {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Retry-After", retryAfterSeconds)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Too Many Requests: rate limit exceeded"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	srv := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: &graph.Resolver{}}))

	http.Handle("/", playground.Handler("GraphQL playground", "/query"))
	http.Handle("/query", rateLimitMiddleware(srv))

	log.Printf("connect to http://localhost:%s/ for GraphQL playground", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
