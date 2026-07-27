package call

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) DeleteCall(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	call, err := s.GetByUUID(ctx, id, userID)
	if err != nil {
		s.log.Warn(ctx, "delete call failed", zap.String("reason", "call_lookup_failed"), zap.String("user_id", userID.String()), zap.String("call_id", id.String()), zap.Error(err))
		return err
	}

	if err := s.repository.DeleteCall(ctx, id, userID); err != nil {
		s.log.Error(ctx, "delete call failed", zap.String("reason", "db_delete_failed"), zap.String("user_id", userID.String()), zap.String("call_id", id.String()), zap.Error(err))
		return err
	}

	if err := s.audioStorage.Delete(ctx, call.AudioPath); err != nil {
		s.log.Error(ctx, "delete call failed", zap.String("reason", "audio_delete_failed"), zap.String("user_id", userID.String()), zap.String("call_id", id.String()), zap.Error(err))
		return fmt.Errorf("delete audio file: %w", err)
	}
	if call.ASRCachePath != "" {
		if err := s.audioStorage.Delete(ctx, call.ASRCachePath); err != nil {
			s.log.Error(ctx, "delete call failed", zap.String("reason", "asr_cache_delete_failed"), zap.String("user_id", userID.String()), zap.String("call_id", id.String()), zap.Error(err))
			return fmt.Errorf("delete ASR cache: %w", err)
		}
	}

	s.log.Info(ctx, "call deleted", zap.String("user_id", userID.String()), zap.String("call_id", id.String()))

	return nil
}
