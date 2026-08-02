package transcription

import "database/sql"

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

const transcriptionReturningColumns = `
	transcription_uuid,
	call_uuid,
	status,
	text,
	segments,
	words,
	language,
	provider,
	error_message,
	created_at,
	updated_at
`
