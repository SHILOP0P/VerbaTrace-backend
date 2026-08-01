package report

import (
	"context"
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) ListByCallUUID(ctx context.Context, callID uuid.UUID, userID uuid.UUID) ([]models.ReportExport, error) {
	if callID == uuid.Nil || userID == uuid.Nil {
		return nil, models.ErrInvalidReportInput
	}

	if _, err := s.callRepository.GetByUUID(ctx, callID, userID); err != nil {
		return nil, err
	}

	return s.reportRepository.ListByCallUUID(ctx, callID, s.now())
}

func (s *Service) GetFile(ctx context.Context, reportID uuid.UUID, userID uuid.UUID) (models.ReportFile, error) {
	if reportID == uuid.Nil || userID == uuid.Nil {
		return models.ReportFile{}, models.ErrInvalidReportInput
	}

	report, err := s.reportRepository.GetByUUID(ctx, reportID)
	if err != nil {
		return models.ReportFile{}, err
	}

	if _, err := s.callRepository.GetByUUID(ctx, report.CallUUID, userID); err != nil {
		return models.ReportFile{}, err
	}

	if !s.now().Before(report.ExpiresAt) {
		return models.ReportFile{}, models.ErrReportExpired
	}
	if report.Status != models.ReportStatusReady {
		return models.ReportFile{}, models.ErrReportNotReady
	}
	if report.StoragePath == nil {
		return models.ReportFile{}, models.ErrReportFileNotFound
	}

	content, err := s.reportStorage.Open(ctx, *report.StoragePath)
	if err != nil {
		return models.ReportFile{}, err
	}

	return models.ReportFile{
		Report:  report,
		Content: content,
	}, nil
}

func (s *Service) Delete(ctx context.Context, reportID uuid.UUID, userID uuid.UUID) error {
	if reportID == uuid.Nil || userID == uuid.Nil {
		return models.ErrInvalidReportInput
	}

	report, err := s.reportRepository.GetByUUID(ctx, reportID)
	if err != nil {
		return err
	}

	if _, err := s.callRepository.GetByUUID(ctx, report.CallUUID, userID); err != nil {
		return err
	}

	if report.StoragePath != nil {
		if err := s.reportStorage.Delete(ctx, *report.StoragePath); err != nil && !errors.Is(err, models.ErrReportFileNotFound) {
			return err
		}
	}

	return s.reportRepository.Delete(ctx, reportID)
}

func (s *Service) DeleteExpired(ctx context.Context, limit int) (int, error) {
	reports, err := s.reportRepository.ListExpiredReady(ctx, s.now(), limit)
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, report := range reports {
		if report.StoragePath != nil {
			if err := s.reportStorage.Delete(ctx, *report.StoragePath); err != nil && !errors.Is(err, models.ErrReportFileNotFound) {
				return deleted, err
			}
		}
		if err := s.reportRepository.Delete(ctx, report.ID); err != nil {
			return deleted, err
		}
		deleted++
	}

	return deleted, nil
}
