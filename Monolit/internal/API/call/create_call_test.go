package call

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestCreateCallSuccess() {
	userID := uuid.New()
	callID := uuid.New()

	body, contentType := multipartBody(s.T(), map[string]string{
		"title": "Test call",
	}, "audio", "call.wav", []byte("RIFF----WAVEfmt "))

	s.service.On("CreateCall", mock.Anything, mock.MatchedBy(func(input models.CreateCallInput) bool {
		return input.Title == "Test call" &&
			input.OriginalFilename == "call.wav" &&
			input.UploadedByUserUUID == userID &&
			input.VisibilityScope == models.CallVisibilityScopePersonal &&
			!input.CompanyUUID.Valid &&
			!input.DepartmentUUID.Valid &&
			!input.SkipCustomInstructions &&
			input.Content != nil
	})).
		Return(models.Call{
			ID:                 callID,
			Title:              "Test call",
			Status:             models.CallStatusNew,
			OriginalFilename:   "call.wav",
			MimeType:           "audio/wave",
			SizeBytes:          16,
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopePersonal,
			CreatedAt:          time.Now().UTC(),
		}, nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestCreateCallAcceptsPeopleAndArbitraryRoles() {
	userID := uuid.New()
	contactID := uuid.New()
	body, contentType := multipartBody(s.T(), map[string]string{
		"title":             "Interview",
		"speaker_hints":     `[{"userId":"` + contactID.String() + `","name":"Анна Иванова","username":"anna","role":"other","note":"Кандидат на Go-разработчика"}]`,
		"diarization_roles": `[{"name":"Технический интервьюер","description":"Задаёт вопросы по Go"},{"name":"HR","description":"Обсуждает условия работы"}]`,
	}, "audio", "call.wav", []byte("RIFF----WAVEfmt "))

	s.service.On("CreateCall", mock.Anything, mock.MatchedBy(func(input models.CreateCallInput) bool {
		return len(input.SpeakerHints) == 1 && input.SpeakerHints[0].Name == "Анна Иванова" &&
			len(input.DiarizationRoles) == 2 && input.DiarizationRoles[0].Name == "Технический интервьюер" && input.DiarizationRoles[1].Name == "HR"
	})).Return(models.Call{ID: uuid.New(), Title: "Interview", Status: models.CallStatusNew, OriginalFilename: "call.wav", MimeType: "audio/wave", UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true}, VisibilityScope: models.CallVisibilityScopePersonal, CreatedAt: time.Now().UTC()}, nil).Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)
	s.api.Create(rec, req)
	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestCreateVideoCallUsingMediaField() {
	userID := uuid.New()
	callID := uuid.New()
	videoHeader := []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'm', 'p', '4', '2'}
	body, contentType := multipartBody(s.T(), map[string]string{
		"title": "Video meeting",
	}, "media", "meeting.mp4", videoHeader)

	s.service.On("CreateCall", mock.Anything, mock.MatchedBy(func(input models.CreateCallInput) bool {
		return input.OriginalFilename == "meeting.mp4" && input.MimeType == "video/mp4"
	})).Return(models.Call{
		ID:                 callID,
		Title:              "Video meeting",
		Status:             models.CallStatusNew,
		OriginalFilename:   "meeting.mp4",
		MimeType:           "video/mp4",
		SizeBytes:          int64(len(videoHeader)),
		UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
		VisibilityScope:    models.CallVisibilityScopePersonal,
		CreatedAt:          time.Now().UTC(),
	}, nil).Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)
	s.api.Create(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
	s.Require().Contains(rec.Body.String(), `"media_kind":"video"`)
	s.Require().Contains(rec.Body.String(), `"media_url":"/api/v1/calls/`)
}

func (s *APISuite) TestCreateCallAcceptsMP3SniffedAsOctetStream() {
	userID := uuid.New()
	callID := uuid.New()
	mp3Header := []byte{0xff, 0xe3, 0x48, 0xc4, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x58, 0x69, 0x6e}

	body, contentType := multipartBody(s.T(), map[string]string{
		"title": "Low bitrate mp3",
	}, "audio", "call.mp3", mp3Header)

	s.service.On("CreateCall", mock.Anything, mock.MatchedBy(func(input models.CreateCallInput) bool {
		return input.OriginalFilename == "call.mp3" && input.MimeType == "audio/mpeg"
	})).
		Return(models.Call{
			ID:                 callID,
			Title:              "Low bitrate mp3",
			Status:             models.CallStatusNew,
			OriginalFilename:   "call.mp3",
			MimeType:           "audio/mpeg",
			SizeBytes:          int64(len(mp3Header)),
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopePersonal,
			CreatedAt:          time.Now().UTC(),
		}, nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestCreateCallAcceptsOGGSniffedAsOctetStream() {
	userID := uuid.New()
	callID := uuid.New()
	oggHeader := []byte("OggS\x00\x02test-audio")

	body, contentType := multipartBody(s.T(), map[string]string{
		"title": "OGG call",
	}, "audio", "call.ogg", oggHeader)

	s.service.On("CreateCall", mock.Anything, mock.MatchedBy(func(input models.CreateCallInput) bool {
		return input.OriginalFilename == "call.ogg" && input.MimeType == "audio/ogg"
	})).
		Return(models.Call{
			ID:                 callID,
			Title:              "OGG call",
			Status:             models.CallStatusNew,
			OriginalFilename:   "call.ogg",
			MimeType:           "audio/ogg",
			SizeBytes:          int64(len(oggHeader)),
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopePersonal,
			CreatedAt:          time.Now().UTC(),
		}, nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestCreateCallCanSkipCustomInstructions() {
	userID := uuid.New()
	body, contentType := multipartBody(s.T(), map[string]string{
		"title":                   "Server only",
		"use_custom_instructions": "false",
	}, "audio", "call.wav", []byte("RIFF----WAVEfmt "))

	s.service.On("CreateCall", mock.Anything, mock.MatchedBy(func(input models.CreateCallInput) bool {
		return input.SkipCustomInstructions
	})).
		Return(models.Call{
			ID:                     uuid.New(),
			Title:                  "Server only",
			Status:                 models.CallStatusNew,
			OriginalFilename:       "call.wav",
			MimeType:               "audio/wave",
			SizeBytes:              16,
			UploadedByUserUUID:     uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:        models.CallVisibilityScopePersonal,
			SkipCustomInstructions: true,
			CreatedAt:              time.Now().UTC(),
		}, nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestCreateCallRequiresAuth() {
	body, contentType := multipartBody(s.T(), map[string]string{
		"title": "Test call",
	}, "audio", "call.wav", []byte("RIFF----WAVEfmt "))

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), uuid.Nil, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestCreateCallRejectsInvalidMultipartForm() {
	rec, req := s.request(http.MethodPost, "/api/v1/calls", "not multipart", uuid.New(), nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=missing")

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidMultipartForm)
}

func (s *APISuite) TestCreateCallUsesFilenameAsTitleWhenTitleIsMissing() {
	body, contentType := multipartBody(s.T(), nil, "audio", "call.wav", []byte("RIFF----WAVEfmt "))
	userID := uuid.New()
	s.service.On("CreateCall", mock.Anything, mock.MatchedBy(func(input models.CreateCallInput) bool {
		return input.Title == "call" && input.OriginalFilename == "call.wav"
	})).Return(models.Call{ID: uuid.New(), Title: "call", Status: models.CallStatusNew, OriginalFilename: "call.wav", UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true}, CreatedAt: time.Now().UTC()}, nil).Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestCreateCallRequiresAudio() {
	body, contentType := multipartBody(s.T(), map[string]string{
		"title": "Test call",
	}, "", "", nil)

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), uuid.New(), nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeAudioFileRequired)
}

func (s *APISuite) TestCreateCallRejectsInvalidPlacement() {
	body, contentType := multipartBody(s.T(), map[string]string{
		"title":           "Test call",
		"department_uuid": uuid.New().String(),
	}, "audio", "call.wav", []byte("RIFF----WAVEfmt "))

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), uuid.New(), nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCallPlacement)
}

func (s *APISuite) TestCreateCallRequiresFileExtension() {
	body, contentType := multipartBody(s.T(), map[string]string{
		"title": "Test call",
	}, "audio", "call", []byte("RIFF----WAVEfmt "))

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), uuid.New(), nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeAudioFileExtensionRequired)
}

func (s *APISuite) TestCreateCallMapsUnsupportedAudioType() {
	userID := uuid.New()
	body, contentType := multipartBody(s.T(), map[string]string{
		"title": "Test call",
	}, "audio", "call.wav", []byte("RIFF----WAVEfmt "))

	s.service.On("CreateCall", mock.Anything, mock.Anything).
		Return(models.Call{}, models.ErrUnsupportedAudioType).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeUnsupportedAudioType)
}

func (s *APISuite) TestCreateCallMapsForbidden() {
	userID := uuid.New()
	body, contentType := multipartBody(s.T(), map[string]string{
		"title":        "Test call",
		"company_uuid": uuid.New().String(),
	}, "audio", "call.wav", []byte("RIFF----WAVEfmt "))

	s.service.On("CreateCall", mock.Anything, mock.Anything).
		Return(models.Call{}, models.ErrForbidden).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusForbidden, rec.Code)
	s.requireErrorCode(rec, response.CodeForbidden)
}

func (s *APISuite) TestCreateCallMapsAudioProbeNotFound() {
	userID := uuid.New()
	body, contentType := multipartBody(s.T(), map[string]string{
		"title": "Test call",
	}, "audio", "call.wav", []byte("RIFF----WAVEfmt "))

	s.service.On("CreateCall", mock.Anything, mock.Anything).
		Return(models.Call{}, models.ErrAudioProbeNotFound).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeAudioProbeNotFound)
}

func (s *APISuite) TestCreateCallMapsAudioFileUnreadable() {
	userID := uuid.New()
	body, contentType := multipartBody(s.T(), map[string]string{
		"title": "Test call",
	}, "audio", "call.wav", []byte("RIFF----WAVEfmt "))

	s.service.On("CreateCall", mock.Anything, mock.Anything).
		Return(models.Call{}, models.ErrAudioFileUnreadable).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeAudioFileUnreadable)
}

func (s *APISuite) TestParseCallPlacementPersonal() {
	companyID, departmentID, scope, err := parseCallPlacement("", "")

	s.Require().NoError(err)
	s.Require().False(companyID.Valid)
	s.Require().False(departmentID.Valid)
	s.Require().Equal(models.CallVisibilityScopePersonal, scope)
}

func (s *APISuite) TestParseCallPlacementCompany() {
	companyUUID := uuid.New()

	companyID, departmentID, scope, err := parseCallPlacement(companyUUID.String(), "")

	s.Require().NoError(err)
	s.Require().True(companyID.Valid)
	s.Require().Equal(companyUUID, companyID.UUID)
	s.Require().False(departmentID.Valid)
	s.Require().Equal(models.CallVisibilityScopeCompany, scope)
}

func (s *APISuite) TestParseCallPlacementDepartment() {
	companyUUID := uuid.New()
	departmentUUID := uuid.New()

	companyID, departmentID, scope, err := parseCallPlacement(companyUUID.String(), departmentUUID.String())

	s.Require().NoError(err)
	s.Require().True(companyID.Valid)
	s.Require().True(departmentID.Valid)
	s.Require().Equal(companyUUID, companyID.UUID)
	s.Require().Equal(departmentUUID, departmentID.UUID)
	s.Require().Equal(models.CallVisibilityScopeDepartment, scope)
}

func (s *APISuite) TestParseCallPlacementRejectsDepartmentWithoutCompany() {
	_, _, _, err := parseCallPlacement("", uuid.New().String())

	s.Require().ErrorIs(err, models.ErrInvalidCallPlacement)
}

func multipartBody(t interface {
	Helper()
	Fatalf(format string, args ...interface{})
}, fields map[string]string, fileField string, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("failed to write multipart field: %v", err)
		}
	}

	if fileField != "" {
		part, err := writer.CreateFormFile(fileField, filename)
		if err != nil {
			t.Fatalf("failed to create multipart file: %v", err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatalf("failed to write multipart file: %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	return body, writer.FormDataContentType()
}

func (s *APISuite) TestParseCallPlacementRejectsInvalidUUID() {
	_, _, _, err := parseCallPlacement("bad uuid", "")

	s.Require().ErrorIs(err, models.ErrInvalidCallPlacement)
}

func (s *APISuite) TestCreateTranscriptionOnlyCall() {
	userID := uuid.New()
	callID := uuid.New()

	body, contentType := multipartBody(s.T(), map[string]string{
		"title":           "Test call",
		"processing_mode": "transcribe",
	}, "audio", "call.wav", []byte("RIFF----WAVEfmt "))

	s.service.On("CreateCall", mock.Anything, mock.MatchedBy(func(input models.CreateCallInput) bool {
		return input.TranscriptionOnly && input.Title == "Test call" &&
			input.OriginalFilename == "call.wav" &&
			input.UploadedByUserUUID == userID &&
			input.VisibilityScope == models.CallVisibilityScopePersonal &&
			!input.CompanyUUID.Valid &&
			!input.DepartmentUUID.Valid &&
			!input.SkipCustomInstructions &&
			input.Content != nil
	})).
		Return(models.Call{
			ID:                 callID,
			Title:              "Test call",
			TranscriptionOnly:  true,
			Status:             models.CallStatusNew,
			OriginalFilename:   "call.wav",
			MimeType:           "audio/wave",
			SizeBytes:          16,
			UploadedByUserUUID: uuid.NullUUID{UUID: userID, Valid: true},
			VisibilityScope:    models.CallVisibilityScopePersonal,
			CreatedAt:          time.Now().UTC(),
		}, nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/calls", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)

	s.api.Create(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}
