package call

import (
	"context"
	"errors"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

type durationDetectorFunc func(ctx context.Context, path string) (int, error)

func (f durationDetectorFunc) DetectDuration(ctx context.Context, path string) (int, error) {
	return f(ctx, path)
}

func validCreateCallInput(userID uuid.UUID) models.CreateCallInput {
	return models.CreateCallInput{
		Title:              "Test call",
		OriginalFilename:   "call.wav",
		MimeType:           "audio/wav",
		SizeBytes:          10,
		Content:            strings.NewReader("audio"),
		UploadedByUserUUID: userID,
		VisibilityScope:    models.CallVisibilityScopePersonal,
	}
}

func (s *ServiceSuite) TestCreateCallSuccessPersonal() {
	userID := uuid.New()
	input := validCreateCallInput(userID)
	savedFile := models.SavedFile{Path: "uploads/call.wav", SizeBytes: input.SizeBytes}

	s.audioStorage.EXPECT().
		Save(mock.Anything, mock.MatchedBy(func(saveInput models.SaveInput) bool {
			return saveInput.OriginalFilename == input.OriginalFilename &&
				saveInput.MimeType == input.MimeType &&
				saveInput.SizeBytes == input.SizeBytes
		})).
		Return(savedFile, nil).
		Once()
	s.repository.EXPECT().
		CreateCall(mock.Anything, mock.MatchedBy(func(call models.Call) bool {
			return call.Title == input.Title &&
				call.AudioPath == savedFile.Path &&
				call.Status == models.CallStatusNew &&
				call.UploadedByUserUUID.Valid &&
				call.UploadedByUserUUID.UUID == userID &&
				call.VisibilityScope == models.CallVisibilityScopePersonal
		})).
		Return(models.Call{
			Title:              input.Title,
			Status:             models.CallStatusNew,
			AudioPath:          savedFile.Path,
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopePersonal,
		}, nil).
		Once()

	got, err := s.service.CreateCall(s.ctx, input)

	s.Require().NoError(err)
	s.Require().Equal(models.CallStatusNew, got.Status)
	s.Require().Equal(savedFile.Path, got.AudioPath)
}

func (s *ServiceSuite) TestCreateCallUsesDetectedDuration() {
	userID := uuid.New()
	input := validCreateCallInput(userID)
	savedFile := models.SavedFile{Path: "uploads/call.wav", SizeBytes: input.SizeBytes}

	s.service.SetDurationDetector(durationDetectorFunc(func(ctx context.Context, path string) (int, error) {
		s.Require().Equal(savedFile.Path, path)
		return 42, nil
	}))
	s.audioStorage.EXPECT().Save(mock.Anything, mock.Anything).Return(savedFile, nil).Once()
	s.repository.EXPECT().
		CreateCall(mock.Anything, mock.MatchedBy(func(call models.Call) bool {
			return call.DurationSeconds == 42
		})).
		Return(models.Call{
			Title:              input.Title,
			Status:             models.CallStatusNew,
			AudioPath:          savedFile.Path,
			DurationSeconds:    42,
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopePersonal,
		}, nil).
		Once()

	got, err := s.service.CreateCall(s.ctx, input)

	s.Require().NoError(err)
	s.Require().Equal(42, got.DurationSeconds)
}

func (s *ServiceSuite) TestCreateCallDeletesSavedFileWhenDurationDetectionFails() {
	input := validCreateCallInput(uuid.New())
	savedFile := models.SavedFile{Path: "uploads/call.wav", SizeBytes: input.SizeBytes}
	durationErr := errors.New("detect duration failed")

	s.service.SetDurationDetector(durationDetectorFunc(func(ctx context.Context, path string) (int, error) {
		return 0, durationErr
	}))
	s.audioStorage.EXPECT().Save(mock.Anything, mock.Anything).Return(savedFile, nil).Once()
	s.audioStorage.EXPECT().Delete(mock.Anything, savedFile.Path).Return(nil).Once()

	_, err := s.service.CreateCall(s.ctx, input)

	s.Require().ErrorIs(err, durationErr)
}

func (s *ServiceSuite) TestCreateCallRejectsInvalidAudioInput() {
	tests := []struct {
		name   string
		mutate func(*models.CreateCallInput)
		want   error
	}{
		{name: "empty owner", mutate: func(input *models.CreateCallInput) { input.UploadedByUserUUID = uuid.Nil }, want: models.ErrInvalidCallOwner},
		{name: "unsupported extension", mutate: func(input *models.CreateCallInput) { input.OriginalFilename = "call.txt" }, want: models.ErrUnsupportedAudioType},
		{name: "unsupported mime", mutate: func(input *models.CreateCallInput) { input.MimeType = "text/plain" }, want: models.ErrUnsupportedAudioType},
		{name: "empty size", mutate: func(input *models.CreateCallInput) { input.SizeBytes = 0 }, want: models.ErrCallConvert},
		{name: "nil content", mutate: func(input *models.CreateCallInput) { input.Content = nil }, want: models.ErrUnsupportedAudioType},
		{name: "invalid personal placement", mutate: func(input *models.CreateCallInput) {
			input.CompanyUUID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
		}, want: models.ErrInvalidCallPlacement},
		{name: "invalid company placement", mutate: func(input *models.CreateCallInput) {
			input.VisibilityScope = models.CallVisibilityScopeCompany
		}, want: models.ErrInvalidCallPlacement},
		{name: "invalid department placement", mutate: func(input *models.CreateCallInput) {
			input.VisibilityScope = models.CallVisibilityScopeDepartment
			input.CompanyUUID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
		}, want: models.ErrInvalidCallPlacement},
		{name: "unknown scope", mutate: func(input *models.CreateCallInput) {
			input.VisibilityScope = models.CallVisibilityScope("unknown")
		}, want: models.ErrInvalidCallPlacement},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			input := validCreateCallInput(uuid.New())
			tt.mutate(&input)

			_, err := s.service.CreateCall(s.ctx, input)

			s.Require().ErrorIs(err, tt.want)
		})
	}
}

func (s *ServiceSuite) TestCreateCallAcceptsOGGAudioInput() {
	input := validCreateCallInput(uuid.New())
	input.OriginalFilename = "call.ogg"

	input.MimeType = "audio/ogg; codecs=opus"
	s.Require().NoError(validateMediaInput(input))

	input.MimeType = "application/ogg"
	s.Require().NoError(validateMediaInput(input))
}

func (s *ServiceSuite) TestCreateCallAcceptsVideoInput() {
	input := validCreateCallInput(uuid.New())
	for _, media := range []struct{ name, mime string }{
		{name: "call.mp4", mime: "video/mp4"},
		{name: "call.mov", mime: "video/quicktime"},
		{name: "call.webm", mime: "video/webm"},
		{name: "call.mkv", mime: "video/x-matroska"},
	} {
		input.OriginalFilename = media.name
		input.MimeType = media.mime
		s.Require().NoError(validateMediaInput(input), media.name)
	}
}

func (s *ServiceSuite) TestCreateCallAllowsCompanyManagerUpload() {
	userID := uuid.New()
	companyID := uuid.New()
	input := validCreateCallInput(userID)
	input.VisibilityScope = models.CallVisibilityScopeCompany
	input.CompanyUUID = uuid.NullUUID{UUID: companyID, Valid: true}

	s.companyRepo.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.audioStorage.EXPECT().
		Save(mock.Anything, mock.Anything).
		Return(models.SavedFile{Path: "uploads/call.wav", SizeBytes: input.SizeBytes}, nil).
		Once()
	s.repository.EXPECT().
		CreateCall(mock.Anything, mock.Anything).
		Return(models.Call{Status: models.CallStatusNew, VisibilityScope: models.CallVisibilityScopeCompany}, nil).
		Once()

	got, err := s.service.CreateCall(s.ctx, input)

	s.Require().NoError(err)
	s.Require().Equal(models.CallVisibilityScopeCompany, got.VisibilityScope)
}

func (s *ServiceSuite) TestCreateCallRejectsCompanyEmployeeUploadToCompanyScope() {
	userID := uuid.New()
	companyID := uuid.New()
	input := validCreateCallInput(userID)
	input.VisibilityScope = models.CallVisibilityScopeCompany
	input.CompanyUUID = uuid.NullUUID{UUID: companyID, Valid: true}

	s.companyRepo.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()

	_, err := s.service.CreateCall(s.ctx, input)

	s.Require().ErrorIs(err, models.ErrForbidden)
}

func (s *ServiceSuite) TestCreateCallAllowsDepartmentMemberUpload() {
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	input := validCreateCallInput(userID)
	input.VisibilityScope = models.CallVisibilityScopeDepartment
	input.CompanyUUID = uuid.NullUUID{UUID: companyID, Valid: true}
	input.DepartmentUUID = uuid.NullUUID{UUID: departmentID, Valid: true}

	s.companyRepo.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).
		Once()
	s.deptRepo.EXPECT().
		GetDepartmentMember(mock.Anything, companyID, departmentID, userID).
		Return(models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: userID, Role: models.DepartmentMemberRoleEmployee}, nil).
		Once()
	s.audioStorage.EXPECT().
		Save(mock.Anything, mock.Anything).
		Return(models.SavedFile{Path: "uploads/call.wav", SizeBytes: input.SizeBytes}, nil).
		Once()
	s.repository.EXPECT().
		CreateCall(mock.Anything, mock.Anything).
		Return(models.Call{Status: models.CallStatusNew, VisibilityScope: models.CallVisibilityScopeDepartment}, nil).
		Once()

	got, err := s.service.CreateCall(s.ctx, input)

	s.Require().NoError(err)
	s.Require().Equal(models.CallVisibilityScopeDepartment, got.VisibilityScope)
}

func (s *ServiceSuite) TestCreateCallDeletesSavedFileWhenRepositoryFails() {
	userID := uuid.New()
	input := validCreateCallInput(userID)
	savedFile := models.SavedFile{Path: "uploads/call.wav", SizeBytes: input.SizeBytes}
	repoErr := errors.New("create failed")

	s.audioStorage.EXPECT().Save(mock.Anything, mock.Anything).Return(savedFile, nil).Once()
	s.repository.EXPECT().CreateCall(mock.Anything, mock.Anything).Return(models.Call{}, repoErr).Once()
	s.audioStorage.EXPECT().Delete(mock.Anything, savedFile.Path).Return(nil).Once()

	_, err := s.service.CreateCall(s.ctx, input)

	s.Require().ErrorIs(err, repoErr)
}

func (s *ServiceSuite) TestCreateCallReturnsStorageSaveError() {
	input := validCreateCallInput(uuid.New())
	storageErr := errors.New("save failed")

	s.audioStorage.EXPECT().Save(mock.Anything, mock.Anything).Return(models.SavedFile{}, storageErr).Once()

	_, err := s.service.CreateCall(s.ctx, input)

	s.Require().ErrorIs(err, storageErr)
}
