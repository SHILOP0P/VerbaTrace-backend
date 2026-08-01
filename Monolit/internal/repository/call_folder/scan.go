package call_folder

import (
	"database/sql"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type folderScanner interface {
	Scan(dest ...any) error
}

func scanFolder(row folderScanner) (models.CallFolder, error) {
	var folder models.CallFolder
	var scope string
	var description sql.NullString
	var color sql.NullString
	var deletedAt sql.NullTime

	err := row.Scan(
		&folder.ID,
		&scope,
		&folder.UserUUID,
		&folder.CompanyUUID,
		&folder.DepartmentUUID,
		&folder.Name,
		&description,
		&color,
		&folder.CallsCount,
		&folder.CreatedByUserUUID,
		&folder.CreatedAt,
		&folder.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return models.CallFolder{}, err
	}

	folder.Scope = models.CallFolderScope(scope)
	if description.Valid {
		folder.Description = &description.String
	}
	if color.Valid {
		folder.Color = &color.String
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		folder.DeletedAt = &t
	}

	return folder, nil
}

func nullUUID(id uuid.NullUUID) any {
	if !id.Valid {
		return nil
	}
	return id.UUID
}

func nullString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
