package report

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) Create(ctx context.Context, input models.CreateReportInput) (models.ReportExport, error) {
	if input.CallUUID == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.ReportExport{}, models.ErrInvalidReportInput
	}

	format, err := normalizeFormat(input.Format)
	if err != nil {
		return models.ReportExport{}, err
	}

	call, err := s.callRepository.GetByUUID(ctx, input.CallUUID, input.UserUUID)
	if err != nil {
		return models.ReportExport{}, err
	}

	if input.Content == "" {
		input.Content = "full"
	}
	if (input.Content != "full" && input.Content != "transcription") || input.TranscriptionRevision < 0 {
		return models.ReportExport{}, models.ErrInvalidReportInput
	}
	if input.Content == "full" {
		if err := s.requireExportAccess(ctx, call, input.UserUUID); err != nil {
			return models.ReportExport{}, err
		}
	}
	var analysis models.CallAnalysis
	if input.Content == "full" {
		analysis, err = s.analysisRepository.GetByCallUUID(ctx, input.CallUUID)
		if err != nil {
			return models.ReportExport{}, err
		}
		if analysis.Status != models.CallAnalysisStatusDone {
			return models.ReportExport{}, models.ErrInvalidAnalysisStatus
		}
	}
	transcriptText := ""
	revision := 0

	loader, supportsVersions := s.transcriptionRepository.(interface {
		GetReportTranscription(context.Context, uuid.UUID, int) (models.Transcription, int, error)
	})
	if supportsVersions {
		transcript, selectedRevision, loadErr := loader.GetReportTranscription(ctx, input.CallUUID, input.TranscriptionRevision)
		if loadErr != nil {
			return models.ReportExport{}, loadErr
		}
		var names map[string]string
		if speakers, ok := s.transcriptionRepository.(interface {
			GetReportSpeakerNames(context.Context, uuid.UUID) (map[string]string, error)
		}); ok {
			names, err = speakers.GetReportSpeakerNames(ctx, input.CallUUID)
			if err != nil {
				return models.ReportExport{}, err
			}
		}
		transcriptText = formatTranscriptWithSpeakers(transcript, names)
		revision = selectedRevision
	} else if input.Content == "transcription" || input.TranscriptionRevision > 0 {
		return models.ReportExport{}, models.ErrInvalidReportInput
	} else {
		transcriptText = s.transcriptionText(ctx, input.CallUUID)
	}

	now := s.now()
	reportID := uuid.New()
	fileName := reportFileName(call.Title, reportID, format)
	report := models.ReportExport{
		Content:               input.Content,
		TranscriptionRevision: revision,
		ID:                    reportID,
		CallUUID:              input.CallUUID,
		AnalysisUUID:          analysis.ID,
		RequestedByUserUUID:   input.UserUUID,
		Format:                format,
		Status:                models.ReportStatusPending,
		FileName:              fileName,
		ContentType:           contentType(format),
		CreatedAt:             now,
		UpdatedAt:             now,
		ExpiresAt:             now.Add(s.retention),
	}

	report, err = s.reportRepository.Create(ctx, report)
	if err != nil {
		return models.ReportExport{}, err
	}

	data := ReportData{
		TranscriptionOnly:     input.Content == "transcription",
		TranscriptionRevision: revision,
		Call:                  call,
		Analysis:              analysis,
		TranscriptionText:     transcriptText,
		GeneratedAt:           now,
	}

	content, err := generateReport(format, data)
	if err != nil {
		return s.markFailed(ctx, report.ID, err)
	}

	saved, err := s.reportStorage.Save(ctx, models.SaveReportInput{
		ReportUUID: report.ID,
		CallUUID:   input.CallUUID,
		Format:     format,
		FileName:   fileName,
		MimeType:   contentType(format),
		Content:    bytes.NewReader(content),
	})
	if err != nil {
		return s.markFailed(ctx, report.ID, err)
	}

	return s.reportRepository.MarkReady(ctx, models.MarkReportReadyInput{
		ID:          report.ID,
		StoragePath: saved.Path,
		FileName:    fileName,
		ContentType: saved.MimeType,
		SizeBytes:   saved.SizeBytes,
	})
}

func (s *Service) requireExportAccess(ctx context.Context, call models.Call, userID uuid.UUID) error {
	if s.billingLimiter == nil {
		return nil
	}

	if call.CompanyUUID.Valid {
		return s.billingLimiter.CanExportReports(ctx, call.CompanyUUID.UUID)
	}

	subscription, err := s.billingLimiter.GetPersonalSubscription(ctx, userID)
	if err != nil {
		return err
	}
	if !subscription.Plan.ExportEnabled {
		return models.ErrExportAccessDenied
	}

	return nil
}

func (s *Service) transcriptionText(ctx context.Context, callID uuid.UUID) string {
	if s.transcriptionRepository == nil {
		return ""
	}

	transcription, err := s.transcriptionRepository.GetByCallUUID(ctx, callID)
	if err != nil {
		return ""
	}
	if transcription.Text != nil && strings.TrimSpace(*transcription.Text) != "" {
		return strings.TrimSpace(*transcription.Text)
	}

	parts := make([]string, 0, len(transcription.Segments))
	for _, segment := range transcription.Segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}
		if segment.Speaker != "" {
			parts = append(parts, segment.Speaker+": "+text)
		} else {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "\n")
}

func (s *Service) markFailed(ctx context.Context, reportID uuid.UUID, cause error) (models.ReportExport, error) {
	message := cause.Error()
	report, err := s.reportRepository.MarkFailed(ctx, models.MarkReportFailedInput{
		ID:           reportID,
		ErrorMessage: message,
	})
	if err != nil {
		return models.ReportExport{}, fmt.Errorf("mark report failed after %w: %w", cause, err)
	}

	return report, cause
}

func reportFileName(title string, id uuid.UUID, format models.ReportFormat) string {
	base := strings.TrimSpace(title)
	if base == "" {
		base = "call-report"
	}

	base = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == ' ' || r == '.' {
			return r
		}
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 'А' && r <= 'я' || r == 'ё' || r == 'Ё' {
			return r
		}
		return '-'
	}, base)

	base = strings.Join(strings.Fields(base), "-")
	if len([]rune(base)) > 60 {
		base = string([]rune(base)[:60])
	}

	return fmt.Sprintf("%s-%s%s", base, id.String(), fileExtension(format))
}
