package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func settleNothing(operationID uuid.UUID) models.SettleCreditsInput {
	usage, _ := json.Marshal(map[string]any{"cancelled": true})
	return models.SettleCreditsInput{OperationUUID: operationID, ActualChargeCredits: 0, ProviderCostNanoUSD: 0, ProviderUsageJSON: usage}
}

func nowUTC() time.Time { return time.Now().UTC() }

// ReleaseCallReservations hands back what a call had booked but never spent.
// Cancelling processing stops the work before the provider is charged, so the
// money has to go back rather than sit reserved until the reconciler notices.
//
// Operations that already reached the provider are left to the reconciler: it
// knows how to find out what actually happened, and guessing here would either
// give away a real charge or keep money that was never spent.
func (r *Repository) ReleaseCallReservations(ctx context.Context, callID uuid.UUID) error {
	rows, err := r.db.QueryContext(ctx,
		`SELECT usage_operation_uuid FROM usage_operations WHERE call_uuid=$1 AND status='reserved'`, callID)
	if err != nil {
		return fmt.Errorf("list call reservations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	operations := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return fmt.Errorf("scan call reservation: %w", err)
		}
		operations = append(operations, id)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("list call reservations: %w", err)
	}

	for _, operationID := range operations {
		// Settling for nothing is exactly "give it all back": the settlement path
		// already knows how to return every unused credit to the grant it came
		// from, and it keeps the ledger balanced while doing so.
		if _, err = r.SettleCredits(ctx, settleNothing(operationID), nowUTC()); err != nil {
			return fmt.Errorf("release call reservation: %w", err)
		}
	}

	return nil
}
