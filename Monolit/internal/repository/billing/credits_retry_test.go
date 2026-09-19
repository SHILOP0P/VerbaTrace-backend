package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// Three analyses of one account reserved credits at once; the database refused
// one reservation for a concurrent update and the whole analysis started over.
// A refusal like that is now tried again; any other error is not.
func TestRetrySerializableTriesAConflictAgain(t *testing.T) {
	calls := 0
	value, err := retrySerializable(context.Background(), "reserve", func() (int, error) {
		calls++
		if calls < 3 {
			return 0, &pgconn.PgError{Code: "40001"}
		}
		return 42, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, value)
	require.Equal(t, 3, calls)

	calls = 0
	broken := errors.New("broken")
	_, err = retrySerializable(context.Background(), "reserve", func() (int, error) {
		calls++
		return 0, broken
	})
	require.ErrorIs(t, err, broken)
	require.Equal(t, 1, calls, "only a serialization conflict is retried")
}
