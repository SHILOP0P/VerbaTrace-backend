package transcription

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

func (r *Repository) GetAnalysisSpeakerContext(ctx context.Context, callID uuid.UUID) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT speaker_key,display_name,role,custom_role FROM call_transcription_speaker_assignments WHERE call_uuid=$1 ORDER BY speaker_key`, callID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []string{}
	for rows.Next() {
		var key, name, role, custom string
		if err = rows.Scan(&key, &name, &role, &custom); err != nil {
			return nil, err
		}
		result = append(result, fmt.Sprintf("Участник %s: имя=%q, назначенная роль=%q, уточнение роли=%q", key, name, role, custom))
	}
	return result, rows.Err()
}
