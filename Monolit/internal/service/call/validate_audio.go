package call

import (
	"path/filepath"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

var allowedMediaExtensions = map[string]struct{}{
	".mp3":  {},
	".wav":  {},
	".m4a":  {},
	".ogg":  {},
	".mp4":  {},
	".mov":  {},
	".webm": {},
	".mkv":  {},
}

var allowedMediaMimeTypes = map[string]struct{}{
	"application/ogg":  {},
	"audio/mpeg":       {},
	"audio/ogg":        {},
	"audio/wav":        {},
	"audio/x-wav":      {},
	"audio/wave":       {},
	"audio/vnd.wave":   {},
	"audio/mp4":        {},
	"audio/x-m4a":      {},
	"video/mp4":        {},
	"video/quicktime":  {},
	"video/webm":       {},
	"video/x-matroska": {},
}

func validateMediaInput(input models.CreateCallInput) error {
	if input.UploadedByUserUUID == uuid.Nil {
		return models.ErrInvalidCallOwner
	}

	ext := strings.ToLower(filepath.Ext(input.OriginalFilename))
	if _, ok := allowedMediaExtensions[ext]; !ok {
		return models.ErrUnsupportedAudioType
	}

	mimeType := strings.ToLower(strings.TrimSpace(strings.Split(input.MimeType, ";")[0]))
	if _, ok := allowedMediaMimeTypes[mimeType]; !ok {
		return models.ErrUnsupportedAudioType
	}

	if input.SizeBytes <= 0 {
		return models.ErrCallConvert
	}

	if input.Content == nil {
		return models.ErrUnsupportedAudioType
	}

	if err := validateCallPlacement(input); err != nil {
		return err
	}

	return nil
}

func validateCallPlacement(input models.CreateCallInput) error {
	switch input.VisibilityScope {
	case models.CallVisibilityScopePersonal:
		if input.CompanyUUID.Valid || input.DepartmentUUID.Valid {
			return models.ErrInvalidCallPlacement
		}
	case models.CallVisibilityScopeCompany:
		if !input.CompanyUUID.Valid || input.DepartmentUUID.Valid {
			return models.ErrInvalidCallPlacement
		}
	case models.CallVisibilityScopeDepartment:
		if !input.CompanyUUID.Valid || !input.DepartmentUUID.Valid {
			return models.ErrInvalidCallPlacement
		}
	default:
		return models.ErrInvalidCallPlacement
	}

	return nil
}
