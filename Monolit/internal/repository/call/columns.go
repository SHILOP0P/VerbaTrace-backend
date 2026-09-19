package call

import (
	"database/sql"
	"errors"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	"verbatrace/monolit/internal/repository/scaner"
)

// IsTestExpr is true for a call that came through a sandbox application. Every
// list that shows calls selects it, so a test call is marked wherever it
// appears, a folder included.
func IsTestExpr(alias string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM ingest_items i JOIN developer_applications a USING(application_uuid) WHERE i.ingest_item_uuid=%s.ingest_item_uuid AND a.environment='sandbox')`, alias)
}

// callColumns is the projection every plain call read uses, so a new column is
// added in one place instead of a dozen queries. It must stay in sync with
// scaner.ScanCall.
var callColumns = `c.call_uuid,
	       c.title,
	       c.status,
	       c.audio_path,
	       c.asr_cache_path,
	       c.original_filename,
	       c.mime_type,
	       c.size_bytes,
	       c.duration_seconds,
	       c.uploaded_by_user_uuid,
	       c.company_uuid,
	       c.department_uuid,
	       c.visibility_scope,
	       c.skip_custom_instructions,
	       c.transcription_only,
	       ` + IsTestExpr("c") + ` AS is_test,
	       c.created_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCallRow(row rowScanner) (model.Call, error) {
	repoCall, err := scaner.ScanCall(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Call{}, model.ErrCallNotFound
		}
		return model.Call{}, fmt.Errorf("scan call: %w", err)
	}

	return converter.RepoCallToModel(repoCall)
}
