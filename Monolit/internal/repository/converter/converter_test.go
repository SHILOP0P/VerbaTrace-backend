package converter

import (
	"database/sql"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
)

func TestConverters(t *testing.T) {
	text := "value"
	now := time.Now().UTC()
	role := models.DepartmentMemberRoleLeader

	mustNoError(t, func() error { _, err := RepoCallAnalysisToModel(repoModel.CallAnalysis{}); return err })
	mustNoError(t, func() error {
		_, err := ModelCallAnalysisToRepoModel(models.CallAnalysis{Model: &text, ResultText: &text, ErrorMessage: &text})
		return err
	})
	if cloneRawMessage(nil) != nil {
		t.Fatal("nil raw message should remain nil")
	}

	mustNoError(t, func() error { _, err := RepoAnalysisInstructionToModel(repoModel.AnalysisInstruction{}); return err })
	mustNoError(t, func() error {
		_, err := RepoAnalysisInstructionsToModels([]repoModel.AnalysisInstruction{{}})
		return err
	})
	mustNoError(t, func() error {
		_, err := ModelAnalysisInstructionToRepoAnalysisInstruction(models.AnalysisInstruction{})
		return err
	})
	mustNoError(t, func() error { _, err := RepoCallToModel(repoModel.Call{}); return err })
	mustNoError(t, func() error { _, err := RepoCallsToModels([]repoModel.Call{{}}); return err })
	mustNoError(t, func() error { _, err := ModelCallToRepoCall(models.Call{}); return err })
	mustNoError(t, func() error { _, err := RepoCompanyToModel(repoModel.Company{}); return err })
	mustNoError(t, func() error { _, err := RepoCompaniesToModels([]repoModel.Company{{}}); return err })
	mustNoError(t, func() error { _, err := ModelCompanyToRepoCompany(models.Company{}); return err })
	mustNoError(t, func() error { _, err := ModelCompanyMemberToRepoCompanyMember(models.CompanyMember{}); return err })
	mustNoError(t, func() error { _, err := RepoCompanyMemberToModel(repoModel.CompanyMember{}); return err })
	mustNoError(t, func() error { _, err := RepoDepartmentToModel(repoModel.Department{}); return err })
	mustNoError(t, func() error { _, err := RepoDepartmentsToModels([]repoModel.Department{{}}); return err })
	mustNoError(t, func() error { _, err := ModelDepartmentToRepoDepartment(models.Department{}); return err })
	mustNoError(t, func() error { _, err := RepoDepartmentMemberToModel(repoModel.DepartmentMember{}); return err })
	mustNoError(t, func() error {
		_, err := ModelDepartmentMemberToRepoDepartmentMember(models.DepartmentMember{})
		return err
	})

	invitation := models.MembershipInvitation{DepartmentRole: &role, RespondedAt: &now}
	repoInvitation, err := ModelInvitationToRepoInvitation(invitation)
	if err != nil || !repoInvitation.DepartmentRole.Valid || !repoInvitation.RespondedAt.Valid {
		t.Fatalf("ModelInvitationToRepoInvitation = %+v, %v", repoInvitation, err)
	}
	modelInvitation, err := RepoInvitationToModel(repoModel.MembershipInvitation{
		DepartmentRole: sql.NullString{String: string(role), Valid: true},
		RespondedAt:    sql.NullTime{Time: now, Valid: true},
	})
	if err != nil || modelInvitation.DepartmentRole == nil || modelInvitation.RespondedAt == nil {
		t.Fatalf("RepoInvitationToModel = %+v, %v", modelInvitation, err)
	}
	mustNoError(t, func() error { _, err := RepoInvitationsToModels([]repoModel.MembershipInvitation{{}}); return err })

	job := models.ProcessingJob{LockedAt: &now, LockedBy: &text, LastError: &text}
	repoJob, err := ModelProcessingJobToRepoModel(job)
	if err != nil || !repoJob.LockedAt.Valid || !repoJob.LockedBy.Valid {
		t.Fatalf("ModelProcessingJobToRepoModel = %+v, %v", repoJob, err)
	}
	mustNoError(t, func() error { _, err := RepoProcessingJobToModel(repoJob); return err })

	session := models.RefreshSession{UserAgent: &text, IPAddress: &text, LastUsedAt: &now, RevokedAt: &now, RevokedReason: &text}
	repoSession, err := ModelRefreshSessionToRepoModel(session)
	if err != nil || !repoSession.UserAgent.Valid || !repoSession.LastUsedAt.Valid {
		t.Fatalf("ModelRefreshSessionToRepoModel = %+v, %v", repoSession, err)
	}
	mustNoError(t, func() error { _, err := RepoRefreshSessionToModel(repoSession); return err })
	if nullTimeToTimePtr(sql.NullTime{}) != nil || timePtrToNullTime(nil).Valid {
		t.Fatal("nil time conversion failed")
	}

	segments := []models.TranscriptionSegment{{Speaker: "A", Text: "hello"}}
	encoded, err := TranscriptionSegmentsToNullString(segments)
	if err != nil || !encoded.Valid {
		t.Fatalf("TranscriptionSegmentsToNullString = %+v, %v", encoded, err)
	}
	decoded, err := nullStringToTranscriptionSegments(encoded)
	if err != nil || len(decoded) != 1 {
		t.Fatalf("nullStringToTranscriptionSegments = %+v, %v", decoded, err)
	}
	if _, err := nullStringToTranscriptionSegments(sql.NullString{String: "{", Valid: true}); err == nil {
		t.Fatal("expected invalid segments error")
	}
	mustNoError(t, func() error {
		_, err := RepoTranscriptionToModel(repoModel.Transcription{Segments: encoded})
		return err
	})
	mustNoError(t, func() error {
		_, err := ModelTranscriptionToRepoModel(models.Transcription{Segments: segments})
		return err
	})

	mustNoError(t, func() error {
		_, err := RepoUserToModel(repoModel.CurrentUserRecord{Post: sql.NullString{String: text, Valid: true}})
		return err
	})
	mustNoError(t, func() error { _, err := ModelUserToRepoModel(models.CurrentUser{Post: &text}); return err })
	if nullStringToStringPtr(sql.NullString{}) != nil || stringPtrToNullString(nil).Valid {
		t.Fatal("nil string conversion failed")
	}
}

func mustNoError(t *testing.T, fn func() error) {
	t.Helper()
	if err := fn(); err != nil {
		t.Fatal(err)
	}
}
