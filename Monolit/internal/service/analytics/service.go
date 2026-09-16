package analytics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/analyzer"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository"
	"verbatrace/monolit/internal/storage"

	"github.com/google/uuid"
)

type Service struct {
	analyticsRepository  repository.AnalyticsRepository
	callFolderRepository repository.CallFolderRepository
	companyRepository    repository.CompanyRepository
	departmentRepository repository.DepartmentRepository
	reportRepository     repository.AggregateReportRepository
	reportStorage        storage.ReportStorage
	analyzer             analyzer.Analyzer
	now                  func() time.Time
	retention            time.Duration
}

func NewService(analyticsRepository repository.AnalyticsRepository) *Service {
	return &Service{analyticsRepository: analyticsRepository, now: func() time.Time { return time.Now().UTC() }, retention: 7 * 24 * time.Hour}
}

func (s *Service) SetCallFolderRepository(repository repository.CallFolderRepository) {
	s.callFolderRepository = repository
}

func (s *Service) SetCompanyRepository(repository repository.CompanyRepository) {
	s.companyRepository = repository
}

func (s *Service) SetDepartmentRepository(repository repository.DepartmentRepository) {
	s.departmentRepository = repository
}

func (s *Service) SetAnalyzer(analyzer analyzer.Analyzer) {
	s.analyzer = analyzer
}

func (s *Service) SetReportRepository(repository repository.AggregateReportRepository) {
	s.reportRepository = repository
}

func (s *Service) SetReportStorage(storage storage.ReportStorage) {
	s.reportStorage = storage
}

func (s *Service) GetOverview(ctx context.Context, input models.AnalyticsOverviewInput) (models.AnalyticsOverview, error) {
	if input.FolderUUID.Valid {
		if s.callFolderRepository == nil {
			return models.AnalyticsOverview{}, models.ErrCallFolderNotFound
		}
		if _, err := s.callFolderRepository.GetVisibleByUUID(ctx, input.FolderUUID.UUID, input.UserID); err != nil {
			return models.AnalyticsOverview{}, err
		}
	}
	return s.analyticsRepository.GetAnalyticsOverview(ctx, input)
}

func (s *Service) CreateDeepAnalysis(ctx context.Context, input models.CreateDeepAnalysisInput) (models.AggregateAnalysis, error) {
	if err := s.normalizeCreateInput(ctx, &input); err != nil {
		return models.AggregateAnalysis{}, err
	}
	if err := s.authorizeCreate(ctx, input); err != nil {
		return models.AggregateAnalysis{}, err
	}
	overviewInput := analyticsInputFromDeepInput(input)
	sources, total, err := s.analyticsRepository.ListAggregateAnalysisSourceCalls(ctx, overviewInput)
	if err != nil {
		return models.AggregateAnalysis{}, err
	}
	if total == 0 {
		return models.AggregateAnalysis{}, models.ErrNoAnalyzedCallsForDeepAnalysis
	}
	request := buildAggregateAnalysisRequest(input, sources, total)
	if !input.Force {
		existing, err := s.analyticsRepository.FindReusableAggregateAnalysis(ctx, input)
		if err == nil && reusableAggregateAnalysisMatchesSourceSet(existing, request) {
			return existing, nil
		}
		if err != nil && !errors.Is(err, models.ErrAggregateAnalysisNotFound) {
			return models.AggregateAnalysis{}, err
		}
	}
	subjectType, subjectID, err := s.limitSubject(ctx, input)
	if err != nil {
		return models.AggregateAnalysis{}, err
	}
	periodStart, periodEnd := utcWeek(input.PeriodFrom)
	if err := s.analyticsRepository.SpendDeepAnalysisUsage(ctx, subjectType, subjectID, periodStart, periodEnd); err != nil {
		return models.AggregateAnalysis{}, err
	}

	now := s.now()
	analysis := models.AggregateAnalysis{
		ID:                uuid.New(),
		Scope:             input.Scope,
		CompanyUUID:       input.CompanyUUID,
		DepartmentUUID:    input.DepartmentUUID,
		FolderUUID:        input.FolderUUID,
		PeriodFrom:        input.PeriodFrom,
		PeriodTo:          input.PeriodTo,
		Status:            models.AggregateAnalysisStatusPending,
		Provider:          s.analyzer.Provider(),
		SourceCallsCount:  total,
		CreatedByUserUUID: input.UserID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if input.Scope == models.AggregateAnalysisScopePersonal {
		analysis.UserUUID = uuid.NullUUID{UUID: input.UserID, Valid: true}
	}
	created, err := s.analyticsRepository.CreateAggregateAnalysis(ctx, analysis)
	if err != nil {
		return models.AggregateAnalysis{}, err
	}

	s.processDeepAnalysisAsync(created.ID, request)
	return created, nil
}

func (s *Service) processDeepAnalysisAsync(id uuid.UUID, request models.AggregateAnalysisRequest) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := s.processDeepAnalysis(ctx, id, request); err != nil {
			_ = err
		}
	}()
}

func (s *Service) processDeepAnalysis(ctx context.Context, id uuid.UUID, request models.AggregateAnalysisRequest) error {
	processing, err := s.analyticsRepository.MarkAggregateAnalysisProcessing(ctx, id)
	if err != nil {
		return err
	}
	result, err := s.analyzer.AnalyzeAggregate(ctx, request)
	if err != nil {
		failed, markErr := s.analyticsRepository.MarkAggregateAnalysisFailed(ctx, processing.ID, err.Error())
		if markErr != nil {
			return markErr
		}
		_ = failed
		return err
	}
	result = enrichAggregateAnalysisResult(result, request)
	_, err = s.analyticsRepository.MarkAggregateAnalysisDone(ctx, processing.ID, result, request.SourceCallsCount)
	return err
}

func (s *Service) ListDeepAnalyses(ctx context.Context, input models.ListDeepAnalysesInput) (models.ListAggregateAnalysesResult, error) {
	if input.Limit <= 0 {
		input.Limit = 20
	}
	if input.Limit > 100 || input.Offset < 0 {
		return models.ListAggregateAnalysesResult{}, models.ErrInvalidDeepAnalysisInput
	}
	if input.Scope != "" && !validAggregateScope(input.Scope) {
		return models.ListAggregateAnalysesResult{}, models.ErrInvalidDeepAnalysisInput
	}
	if input.Status != "" && !validAggregateStatus(input.Status) {
		return models.ListAggregateAnalysesResult{}, models.ErrInvalidDeepAnalysisInput
	}
	return s.analyticsRepository.ListAggregateAnalyses(ctx, input)
}

func (s *Service) GetDeepAnalysis(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.AggregateAnalysis, error) {
	analysis, err := s.analyticsRepository.GetAggregateAnalysisByUUID(ctx, id)
	if err != nil {
		return models.AggregateAnalysis{}, err
	}
	if err := s.authorizeRead(ctx, analysis, userID); err != nil {
		return models.AggregateAnalysis{}, models.ErrAggregateAnalysisNotFound
	}
	return analysis, nil
}

func (s *Service) CreateAggregateReport(ctx context.Context, input models.CreateAggregateReportInput) (models.AggregateReportExport, error) {
	if input.AggregateAnalysisUUID == uuid.Nil || input.UserUUID == uuid.Nil || s.reportRepository == nil || s.reportStorage == nil {
		return models.AggregateReportExport{}, models.ErrInvalidAggregateReportInput
	}
	format, err := normalizeReportFormat(input.Format)
	if err != nil {
		return models.AggregateReportExport{}, err
	}
	analysis, err := s.GetDeepAnalysis(ctx, input.AggregateAnalysisUUID, input.UserUUID)
	if err != nil {
		return models.AggregateReportExport{}, err
	}
	if analysis.Status != models.AggregateAnalysisStatusDone {
		return models.AggregateReportExport{}, models.ErrInvalidAnalysisStatus
	}
	now := s.now()
	reportID := uuid.New()
	fileName := aggregateReportFileName(analysis, reportID, format)
	report := models.AggregateReportExport{
		ID: reportID, AggregateAnalysisUUID: analysis.ID, RequestedByUserUUID: input.UserUUID,
		Format: format, Status: models.ReportStatusPending, FileName: fileName, ContentType: reportContentType(format),
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(s.retention),
	}
	report, err = s.reportRepository.CreateAggregate(ctx, report)
	if err != nil {
		return models.AggregateReportExport{}, err
	}
	content, err := generateAggregateReport(format, AggregateReportData{Analysis: analysis, GeneratedAt: now})
	if err != nil {
		return s.markAggregateReportFailed(ctx, report.ID, err)
	}
	saved, err := s.reportStorage.Save(ctx, models.SaveReportInput{
		ReportUUID: report.ID, AggregateAnalysisUUID: analysis.ID, Format: format,
		FileName: fileName, MimeType: reportContentType(format), Content: bytes.NewReader(content),
	})
	if err != nil {
		return s.markAggregateReportFailed(ctx, report.ID, err)
	}
	return s.reportRepository.MarkAggregateReady(ctx, models.MarkAggregateReportReadyInput{
		ID: report.ID, StoragePath: saved.Path, FileName: fileName, ContentType: saved.MimeType, SizeBytes: saved.SizeBytes,
	})
}

func (s *Service) ListAggregateReports(ctx context.Context, analysisID uuid.UUID, userID uuid.UUID) ([]models.AggregateReportExport, error) {
	if analysisID == uuid.Nil || userID == uuid.Nil || s.reportRepository == nil {
		return nil, models.ErrInvalidAggregateReportInput
	}
	if _, err := s.GetDeepAnalysis(ctx, analysisID, userID); err != nil {
		return nil, err
	}
	return s.reportRepository.ListAggregateByAnalysisUUID(ctx, analysisID, s.now())
}

func (s *Service) GetAggregateReportFile(ctx context.Context, reportID uuid.UUID, userID uuid.UUID) (models.AggregateReportFile, error) {
	if reportID == uuid.Nil || userID == uuid.Nil || s.reportRepository == nil || s.reportStorage == nil {
		return models.AggregateReportFile{}, models.ErrInvalidAggregateReportInput
	}
	report, err := s.reportRepository.GetAggregateByUUID(ctx, reportID)
	if err != nil {
		return models.AggregateReportFile{}, err
	}
	if _, err := s.GetDeepAnalysis(ctx, report.AggregateAnalysisUUID, userID); err != nil {
		return models.AggregateReportFile{}, models.ErrAggregateReportNotFound
	}
	if !s.now().Before(report.ExpiresAt) {
		return models.AggregateReportFile{}, models.ErrReportExpired
	}
	if report.Status != models.ReportStatusReady {
		return models.AggregateReportFile{}, models.ErrReportNotReady
	}
	if report.StoragePath == nil {
		return models.AggregateReportFile{}, models.ErrAggregateReportFileNotFound
	}
	content, err := s.reportStorage.Open(ctx, *report.StoragePath)
	if err != nil {
		if errors.Is(err, models.ErrReportFileNotFound) {
			return models.AggregateReportFile{}, models.ErrAggregateReportFileNotFound
		}
		return models.AggregateReportFile{}, err
	}
	return models.AggregateReportFile{Report: report, Content: content}, nil
}

func (s *Service) DeleteAggregateReport(ctx context.Context, reportID uuid.UUID, userID uuid.UUID) error {
	if reportID == uuid.Nil || userID == uuid.Nil || s.reportRepository == nil || s.reportStorage == nil {
		return models.ErrInvalidAggregateReportInput
	}
	report, err := s.reportRepository.GetAggregateByUUID(ctx, reportID)
	if err != nil {
		return err
	}
	if _, err := s.GetDeepAnalysis(ctx, report.AggregateAnalysisUUID, userID); err != nil {
		return models.ErrAggregateReportNotFound
	}
	if report.StoragePath != nil {
		if err := s.reportStorage.Delete(ctx, *report.StoragePath); err != nil && !errors.Is(err, models.ErrReportFileNotFound) {
			return err
		}
	}
	return s.reportRepository.DeleteAggregate(ctx, reportID)
}

func (s *Service) markAggregateReportFailed(ctx context.Context, reportID uuid.UUID, cause error) (models.AggregateReportExport, error) {
	report, err := s.reportRepository.MarkAggregateFailed(ctx, models.MarkAggregateReportFailedInput{ID: reportID, ErrorMessage: cause.Error()})
	if err != nil {
		return models.AggregateReportExport{}, fmt.Errorf("mark aggregate report failed after %w: %w", cause, err)
	}
	return report, cause
}

func (s *Service) normalizeCreateInput(ctx context.Context, input *models.CreateDeepAnalysisInput) error {
	if s.analyzer == nil || !validAggregateScope(input.Scope) || input.UserID == uuid.Nil || input.PeriodFrom.IsZero() || input.PeriodTo.IsZero() || input.PeriodFrom.After(input.PeriodTo) {
		return models.ErrInvalidDeepAnalysisInput
	}
	input.PeriodFrom = input.PeriodFrom.UTC()
	input.PeriodTo = input.PeriodTo.UTC()
	if input.FolderUUID.Valid {
		if s.callFolderRepository == nil {
			return models.ErrInvalidDeepAnalysisInput
		}
		folder, err := s.callFolderRepository.GetByUUID(ctx, input.FolderUUID.UUID)
		if err != nil {
			return err
		}
		input.Scope = models.AggregateAnalysisScopeFolder
		input.CompanyUUID = folder.CompanyUUID
		input.DepartmentUUID = folder.DepartmentUUID
		return nil
	}
	switch input.Scope {
	case models.AggregateAnalysisScopePersonal:
		if input.CompanyUUID.Valid || input.DepartmentUUID.Valid {
			return models.ErrInvalidDeepAnalysisInput
		}
	case models.AggregateAnalysisScopeCompany:
		if !input.CompanyUUID.Valid || input.DepartmentUUID.Valid {
			return models.ErrInvalidDeepAnalysisInput
		}
	case models.AggregateAnalysisScopeDepartment:
		if !input.CompanyUUID.Valid || !input.DepartmentUUID.Valid {
			return models.ErrInvalidDeepAnalysisInput
		}
	case models.AggregateAnalysisScopeFolder:
		return models.ErrInvalidDeepAnalysisInput
	}
	return nil
}

func (s *Service) authorizeCreate(ctx context.Context, input models.CreateDeepAnalysisInput) error {
	switch input.Scope {
	case models.AggregateAnalysisScopePersonal:
		return nil
	case models.AggregateAnalysisScopeCompany:
		return s.requireCompanyManager(ctx, input.CompanyUUID.UUID, input.UserID)
	case models.AggregateAnalysisScopeDepartment:
		return s.requireDepartmentLeaderOrCompanyManager(ctx, input.CompanyUUID.UUID, input.DepartmentUUID.UUID, input.UserID)
	case models.AggregateAnalysisScopeFolder:
		folder, err := s.callFolderRepository.GetByUUID(ctx, input.FolderUUID.UUID)
		if err != nil {
			return err
		}
		switch folder.Scope {
		case models.CallFolderScopePersonal:
			if folder.UserUUID.Valid && folder.UserUUID.UUID == input.UserID {
				return nil
			}
			return models.ErrForbidden
		case models.CallFolderScopeCompany:
			return s.requireCompanyManager(ctx, folder.CompanyUUID.UUID, input.UserID)
		case models.CallFolderScopeDepartment:
			return s.requireDepartmentLeaderOrCompanyManager(ctx, folder.CompanyUUID.UUID, folder.DepartmentUUID.UUID, input.UserID)
		}
	}
	return models.ErrInvalidDeepAnalysisInput
}

func (s *Service) authorizeRead(ctx context.Context, analysis models.AggregateAnalysis, userID uuid.UUID) error {
	if analysis.UserUUID.Valid && analysis.UserUUID.UUID == userID {
		return nil
	}
	if analysis.CompanyUUID.Valid {
		if member, err := s.companyRepository.GetCompanyMember(ctx, analysis.CompanyUUID.UUID, userID); err == nil && member.Status == models.MembershipStatusActive {
			return nil
		}
	}
	if analysis.DepartmentUUID.Valid {
		if _, err := s.departmentRepository.GetDepartmentMember(ctx, analysis.CompanyUUID.UUID, analysis.DepartmentUUID.UUID, userID); err == nil {
			return nil
		}
	}
	return models.ErrForbidden
}

func (s *Service) requireCompanyManager(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil || !member.Role.ManagesCompany() || member.Status != models.MembershipStatusActive {
		return models.ErrForbidden
	}
	return nil
}

func (s *Service) requireDepartmentLeaderOrCompanyManager(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID) error {
	if err := s.requireCompanyManager(ctx, companyID, userID); err == nil {
		return nil
	}
	member, err := s.departmentRepository.GetDepartmentMember(ctx, companyID, departmentID, userID)
	if err != nil || member.Role != models.DepartmentMemberRoleLeader || member.Status != models.MembershipStatusActive {
		return models.ErrForbidden
	}
	return nil
}

func (s *Service) limitSubject(ctx context.Context, input models.CreateDeepAnalysisInput) (models.DeepAnalysisSubjectType, uuid.UUID, error) {
	if input.Scope == models.AggregateAnalysisScopePersonal {
		return models.DeepAnalysisSubjectTypeUser, input.UserID, nil
	}
	if input.Scope == models.AggregateAnalysisScopeFolder {
		folder, err := s.callFolderRepository.GetByUUID(ctx, input.FolderUUID.UUID)
		if err != nil {
			return "", uuid.Nil, err
		}
		if folder.Scope == models.CallFolderScopePersonal {
			return models.DeepAnalysisSubjectTypeUser, input.UserID, nil
		}
		return models.DeepAnalysisSubjectTypeCompany, folder.CompanyUUID.UUID, nil
	}
	return models.DeepAnalysisSubjectTypeCompany, input.CompanyUUID.UUID, nil
}

func analyticsInputFromDeepInput(input models.CreateDeepAnalysisInput) models.AnalyticsOverviewInput {
	visibilityScope := models.CallVisibilityScope(input.Scope)
	if input.Scope == models.AggregateAnalysisScopeFolder {
		visibilityScope = ""
	}
	return models.AnalyticsOverviewInput{
		UserID: input.UserID, VisibilityScope: visibilityScope, CompanyUUID: input.CompanyUUID,
		DepartmentUUID: input.DepartmentUUID, From: &input.PeriodFrom, To: &input.PeriodTo, FolderUUID: input.FolderUUID,
	}
}

func utcWeek(t time.Time) (time.Time, time.Time) {
	d := t.UTC()
	weekday := int(d.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(weekday - 1))
	return start, start.AddDate(0, 0, 7).Add(-time.Nanosecond)
}

func validAggregateScope(scope models.AggregateAnalysisScope) bool {
	switch scope {
	case models.AggregateAnalysisScopePersonal, models.AggregateAnalysisScopeCompany, models.AggregateAnalysisScopeDepartment, models.AggregateAnalysisScopeFolder:
		return true
	default:
		return false
	}
}

func validAggregateStatus(status models.AggregateAnalysisStatus) bool {
	switch status {
	case models.AggregateAnalysisStatusPending, models.AggregateAnalysisStatusProcessing, models.AggregateAnalysisStatusDone, models.AggregateAnalysisStatusFailed:
		return true
	default:
		return false
	}
}
