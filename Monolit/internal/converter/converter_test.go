package converter

import (
	"encoding/json"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func TestCoreConverters(t *testing.T) {
	now := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	id := uuid.New()
	optionalID := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	text := "text"

	user, err := UserModelToAPI(models.CurrentUser{ID: id, Email: "user@example.com", Role: models.UserRoleUser, Post: &text, CreatedAt: now})
	if err != nil || user.ID != id.String() || user.Email != "user@example.com" {
		t.Fatalf("UserModelToAPI = %+v, %v", user, err)
	}

	call, err := CreateAPIToModel(id, "title", models.CallStatusNew, "path", "call.wav", "audio/wav", 42, now)
	if err != nil || call.ID != id || call.VisibilityScope != models.CallVisibilityScopePersonal {
		t.Fatalf("CreateAPIToModel = %+v, %v", call, err)
	}
	savedCall, err := SavedFileToModel(models.SavedFile{Path: "saved.wav", SizeBytes: 10}, id, models.CreateCallInput{
		Title: "saved", UploadedByUserUUID: uuid.New(), CompanyUUID: optionalID,
		VisibilityScope: models.CallVisibilityScopeCompany,
	}, now)
	if err != nil || !savedCall.UploadedByUserUUID.Valid || savedCall.AudioPath != "saved.wav" {
		t.Fatalf("SavedFileToModel = %+v, %v", savedCall, err)
	}
	callResponse, err := CallModelToAPI(savedCall)
	if err != nil || callResponse.UploadedByUserUUID == nil || callResponse.CompanyUUID == nil || callResponse.DepartmentUUID != nil || callResponse.AudioURL != "/api/v1/calls/"+id.String()+"/audio" {
		t.Fatalf("CallModelToAPI = %+v, %v", callResponse, err)
	}

	analysisResponse, err := AnalysisModelToAPI(models.CallAnalysis{
		ID: id, CallUUID: uuid.New(), Status: models.CallAnalysisStatusDone,
		ResultJSON: json.RawMessage(`{"ok":true}`), ResultText: &text, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || analysisResponse.ID != id.String() {
		t.Fatalf("AnalysisModelToAPI = %+v, %v", analysisResponse, err)
	}

	transcriptionResponse, err := TranscriptionModelToAPI(models.Transcription{
		ID: id, CallUUID: uuid.New(), Status: models.TranscriptionStatusTranscribed, Text: &text,
		Segments: []models.TranscriptionSegment{{Speaker: "A", Text: "hello"}}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || len(transcriptionResponse.Segments) != 1 {
		t.Fatalf("TranscriptionModelToAPI = %+v, %v", transcriptionResponse, err)
	}

	instruction := models.AnalysisInstruction{
		ID: id, Scope: models.AnalysisInstructionScopeCompany, CompanyUUID: optionalID,
		CreatedByUserUUID: uuid.New(), CreatedAt: now, UpdatedAt: now,
	}
	instructionResponse, err := AnalysisInstructionModelToAPI(instruction)
	if err != nil || instructionResponse.CompanyUUID == nil || instructionResponse.UserUUID != nil {
		t.Fatalf("AnalysisInstructionModelToAPI = %+v, %v", instructionResponse, err)
	}
	rawInstruction, err := json.Marshal(instructionResponse)
	if err != nil {
		t.Fatal(err)
	}
	if string(rawInstruction) == "" || json.Valid(rawInstruction) && containsJSONField(rawInstruction, "file_path") {
		t.Fatalf("instruction response leaked file_path: %s", rawInstruction)
	}
	if instructionResponse.DownloadURL != "/api/v1/instructions/"+id.String()+"/download" {
		t.Fatalf("download_url = %q", instructionResponse.DownloadURL)
	}
	instructions, err := AnalysisInstructionModelsToAPI([]models.AnalysisInstruction{instruction})
	if err != nil || len(instructions) != 1 {
		t.Fatalf("AnalysisInstructionModelsToAPI = %+v, %v", instructions, err)
	}
}

func containsJSONField(raw []byte, field string) bool {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	_, ok := fields[field]
	return ok
}

func TestBillingCompanyInvitationAndReportConverters(t *testing.T) {
	now := time.Date(2026, time.June, 22, 10, 0, 0, 123, time.FixedZone("test", 3*60*60))
	id := uuid.New()
	otherID := uuid.New()
	optionalID := uuid.NullUUID{UUID: otherID, Valid: true}

	plan := models.Plan{ID: id, Code: models.PlanCodePersonalPro, Type: models.PlanTypePersonal, AnalysisLevel: models.AnalysisLevelPro}
	planResponse, err := PlanModelToAPI(plan)
	if err != nil || planResponse.ID != id.String() {
		t.Fatalf("PlanModelToAPI = %+v, %v", planResponse, err)
	}
	subscriptionResponse, err := SubscriptionModelToAPI(models.Subscription{
		ID: id, Plan: plan, UserUUID: optionalID, CompanyUUID: optionalID,
		Status: models.SubscriptionStatusActive, StartsAt: now, EndsAt: &now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || subscriptionResponse.UserUUID == nil || subscriptionResponse.CompanyUUID == nil || subscriptionResponse.EndsAt == nil {
		t.Fatalf("SubscriptionModelToAPI = %+v, %v", subscriptionResponse, err)
	}
	if _, err := SubscriptionModelToAPI(models.Subscription{Plan: plan}); err != nil {
		t.Fatalf("SubscriptionModelToAPI zero optionals: %v", err)
	}

	company := models.Company{ID: id, ManagerUserUUID: otherID, CreatedAt: now}
	department := models.Department{ID: otherID, CompanyUUID: id, CreatedAt: now}
	member := models.CompanyMember{CompanyUUID: id, UserUUID: otherID, Role: models.CompanyMemberRoleEmployee, CreatedAt: now}
	departmentMember := models.DepartmentMember{DepartmentUUID: otherID, UserUUID: id, Role: models.DepartmentMemberRoleEmployee, CreatedAt: now}
	companyResponse, err := CompanyModelToAPI(company)
	if err != nil {
		t.Fatal(err)
	}
	if companyResponse.Tag != "@"+id.String() {
		t.Fatalf("default company tag = %q", companyResponse.Tag)
	}
	if _, err := DepartmentModelToAPI(department); err != nil {
		t.Fatal(err)
	}
	if _, err := CompanyMemberModelToAPI(member); err != nil {
		t.Fatal(err)
	}
	if _, err := DepartmentMemberModelToAPI(departmentMember); err != nil {
		t.Fatal(err)
	}
	overview, err := CompanyMembersOverviewModelToAPI(models.CompanyMembersOverview{
		CompanyUUID:      id,
		Manager:          &member,
		CompanyEmployees: []models.CompanyMember{member},
		Departments: []models.DepartmentMembersOverview{{
			Department: department,
			Members:    []models.DepartmentMember{departmentMember},
		}},
	})
	if err != nil || overview.Manager == nil || len(overview.CompanyEmployees) != 1 || len(overview.Departments) != 1 {
		t.Fatalf("CompanyMembersOverviewModelToAPI = %+v, %v", overview, err)
	}

	role := models.DepartmentMemberRoleLeader
	invitation := models.MembershipInvitation{
		ID: id, CompanyUUID: otherID, DepartmentUUID: optionalID, InvitedUserUUID: uuid.New(),
		InvitedByUserUUID: uuid.New(), CompanyRole: models.CompanyMemberRoleEmployee, DepartmentRole: &role,
		Status: models.InvitationStatusAccepted, ExpiresAt: now, RespondedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	invitationResponse, err := InvitationModelToAPI(invitation)
	if err != nil || invitationResponse.DepartmentUUID == nil || invitationResponse.DepartmentRole == nil || invitationResponse.RespondedAt == nil {
		t.Fatalf("InvitationModelToAPI = %+v, %v", invitationResponse, err)
	}
	invitations, err := InvitationsModelToAPI([]models.MembershipInvitation{invitation, {}})
	if err != nil || len(invitations) != 2 {
		t.Fatalf("InvitationsModelToAPI = %+v, %v", invitations, err)
	}

	report := models.ReportExport{
		ID: id, CallUUID: otherID, AnalysisUUID: uuid.New(), RequestedByUserUUID: uuid.New(),
		Status: models.ReportStatusReady, Format: models.ReportFormatPDF, CreatedAt: now, UpdatedAt: now, ExpiresAt: now,
	}
	reportResponse, err := ReportModelToAPI(report)
	if err != nil || reportResponse.DownloadURL == nil {
		t.Fatalf("ReportModelToAPI = %+v, %v", reportResponse, err)
	}
	report.Status = models.ReportStatusPending
	reports, err := ReportsModelToAPI([]models.ReportExport{report})
	if err != nil || len(reports.Reports) != 1 || reports.Reports[0].DownloadURL != nil {
		t.Fatalf("ReportsModelToAPI = %+v, %v", reports, err)
	}
}
