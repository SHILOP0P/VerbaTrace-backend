package report

import "database/sql"

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

const reportColumns = `
	report_uuid,
	call_uuid,
	analysis_uuid,
	requested_by_user_uuid,
	format,
	status,
	storage_path,
	file_name,
	content_type,
	size_bytes,
	error_message,
	created_at,
	updated_at,
	expires_at,
 content,
 transcription_revision
`
