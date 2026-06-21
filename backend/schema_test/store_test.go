//go:build database

package schematest

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/store/pgstore"
)

// TestStoreConnectsAndQueries is the Phase 5 store exit criterion: the real
// pgstore connects to the database and runs a trivial query. It builds a pool
// against the throwaway DB and calls CountLabs — the same code path runs against
// Neon when DATABASE_URL points there.
func TestStoreConnectsAndQueries(t *testing.T) {
	url := os.Getenv("SCHEMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SCHEMA_TEST_DATABASE_URL not set; run via `mage testSchema`")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	s, err := pgstore.New(ctx, pool) // pings on construction
	require.NoError(t, err)

	n, err := s.CountLabs(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 1, "the demo seed creates at least one lab")
}
