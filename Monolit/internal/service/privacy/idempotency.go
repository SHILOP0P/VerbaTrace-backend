package privacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrIdempotencyKeyReused = errors.New("idempotency key reused")
var ErrIdempotencyInProgress = errors.New("idempotency operation in progress")

type IdempotencyReplay struct {
	Status int
	Body   json.RawMessage
}

func (s *Service) BeginIdempotentMutation(ctx context.Context, actorID uuid.UUID, operation, key string, request any) (*IdempotencyReplay, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(body)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `INSERT INTO privacy_mutation_idempotency(actor_user_uuid,operation,idempotency_key,request_sha256,state,expires_at) VALUES($1,$2,$3,$4,'processing',$5) ON CONFLICT DO NOTHING`, actorID, operation, key, hash[:], s.now().UTC().Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	inserted, _ := result.RowsAffected()
	if inserted == 1 {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	var existingHash []byte
	var state string
	var status sql.NullInt64
	var responseBody []byte
	if err = tx.QueryRowContext(ctx, `SELECT request_sha256,state,response_status,response_body FROM privacy_mutation_idempotency WHERE actor_user_uuid=$1 AND operation=$2 AND idempotency_key=$3 FOR UPDATE`, actorID, operation, key).Scan(&existingHash, &state, &status, &responseBody); err != nil {
		return nil, err
	}
	if !bytes.Equal(existingHash, hash[:]) {
		return nil, ErrIdempotencyKeyReused
	}
	if state == "completed" && status.Valid {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return &IdempotencyReplay{Status: int(status.Int64), Body: responseBody}, nil
	}
	if state == "failed_retryable" {
		_, err = tx.ExecContext(ctx, `UPDATE privacy_mutation_idempotency SET state='processing',response_status=NULL,response_body=NULL,expires_at=$4 WHERE actor_user_uuid=$1 AND operation=$2 AND idempotency_key=$3`, actorID, operation, key, s.now().UTC().Add(24*time.Hour))
		if err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return nil, ErrIdempotencyInProgress
}

func (s *Service) CompleteIdempotentMutation(ctx context.Context, actorID uuid.UUID, operation, key string, status int, response any) error {
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE privacy_mutation_idempotency SET state='completed',response_status=$4,response_body=$5::jsonb WHERE actor_user_uuid=$1 AND operation=$2 AND idempotency_key=$3 AND state='processing'`, actorID, operation, key, status, body)
	return err
}
func (s *Service) FailIdempotentMutation(ctx context.Context, actorID uuid.UUID, operation, key string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE privacy_mutation_idempotency SET state='failed_retryable' WHERE actor_user_uuid=$1 AND operation=$2 AND idempotency_key=$3 AND state='processing'`, actorID, operation, key)
}
