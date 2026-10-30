package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/alvor-technologies/iag-platform-go/ratelimit"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newRateLimitStore returns the limiter's store for RATE_LIMIT_STORE. A nil
// store keeps the middleware's in-memory default, which counts per replica: with
// N replicas behind the load balancer a caller effectively gets N times the
// budget. "postgres" shares one counter row per caller across replicas.
//
// The returned loop prunes finished windows; run it in the background.
func newRateLimitStore(ctx context.Context, kind string, pool *pgxpool.Pool) (ratelimit.Store, func(context.Context), error) {
	switch kind {
	case "", "memory":
		return nil, nil, nil
	case "postgres":
		pg := ratelimit.NewPostgresStore(pool)
		if err := pg.EnsureSchema(ctx); err != nil {
			return nil, nil, fmt.Errorf("create rate-limit table: %w", err)
		}
		cleanup := func(ctx context.Context) {
			t := time.NewTicker(time.Hour)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					// Every replica runs this; the DELETE is idempotent.
					if _, err := pg.Cleanup(ctx, time.Hour); err != nil {
						log.Printf("rate-limit cleanup: %v", err)
					}
				}
			}
		}
		return pg, cleanup, nil
	default:
		return nil, nil, fmt.Errorf("RATE_LIMIT_STORE=%q: want memory or postgres", kind)
	}
}
