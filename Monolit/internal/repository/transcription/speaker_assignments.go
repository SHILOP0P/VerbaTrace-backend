package transcription

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// MergeDetectedSpeakerAssignments adds only previously unknown speakers. A
// user-selected name or role is never overwritten by automatic detection.
func (r *Repository) MergeDetectedSpeakerAssignments(ctx context.Context, callID, updatedBy uuid.UUID, names map[string]string) error {
	for speaker, name := range names {
		speaker, name = strings.TrimSpace(speaker), strings.TrimSpace(name)
		if speaker == "" || name == "" {
			continue
		}
		_, err := r.db.ExecContext(ctx, `
			INSERT INTO call_transcription_speaker_assignments(call_uuid,speaker_key,display_name,role,custom_role,updated_by_user_uuid)
			VALUES($1,$2,$3,'unknown','',$4)
			ON CONFLICT(call_uuid,speaker_key) DO NOTHING
		`, callID, speaker, name, updatedBy)
		if err != nil {
			return fmt.Errorf("merge detected speaker %s: %w", speaker, err)
		}
	}
	return nil
}
