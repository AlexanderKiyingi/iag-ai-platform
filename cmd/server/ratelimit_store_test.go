package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/alvor-technologies/iag-platform-go/ratelimit"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewRateLimitStoreKinds(t *testing.T) {
	for _, kind := range []string{"", "memory"} {
		store, cleanup, err := newRateLimitStore(context.Background(), kind, nil)
		if err != nil || store != nil || cleanup != nil {
			t.Errorf("%q: store=%v cleanup=%v err=%v, want the in-memory default", kind, store, cleanup != nil, err)
		}
	}
	if _, _, err := newRateLimitStore(context.Background(), "redis", nil); err == nil {
		t.Error("unknown RATE_LIMIT_STORE accepted")
	}
}

// Two replicas sharing the postgres store must enforce one budget between
// them: a caller alternating replicas gets the limit, not twice the limit.
// Skipped unless TEST_DATABASE_URL is set.
func TestPostgresRateLimitSharedAcrossReplicas(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	gin.SetMode(gin.TestMode)
	const limit = 5
	// Unique per run: counters persist in the shared table for the window.
	bucket := fmt.Sprintf("shared-replica-test-%d", time.Now().UnixNano())
	replica := func() http.Handler {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		store, _, err := newRateLimitStore(ctx, "postgres", pool)
		if err != nil {
			t.Fatal(err)
		}
		r := gin.New()
		r.Use(ratelimit.Middleware(ratelimit.Config{
			Rate:  ratelimit.PerMinute(limit),
			Key:   func(*gin.Context) string { return bucket },
			Store: store,
		}))
		r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
		return r
	}
	replicas := []http.Handler{replica(), replica()}

	allowed := 0
	for i := 0; i < 4*limit; i++ {
		rec := httptest.NewRecorder()
		replicas[i%2].ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code == http.StatusOK {
			allowed++
		}
	}
	if allowed != limit {
		t.Fatalf("allowed %d requests across 2 replicas, want %d", allowed, limit)
	}
}
