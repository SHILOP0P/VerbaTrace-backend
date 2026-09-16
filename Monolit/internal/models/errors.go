package models

import (
	"errors"

	"github.com/google/uuid"
)

// CALL
var ErrCallNotFound = errors.New("call not found")
var ErrCallConvert = errors.New("call convert error")
var ErrUnsupportedAudioType = errors.New("unsupported audio type")
var ErrInvalidCallTitle = errors.New("invalid call title")
var ErrInvalidCallOwner = errors.New("invalid call owner")
var ErrInvalidCallPlacement = errors.New("invalid call placement")
var ErrInvalidCallFilter = errors.New("invalid call filter")
var ErrInvalidCallStatus = errors.New("invalid call status")
var ErrInvalidCallStatusTransition = errors.New("invalid call status transition")
var ErrCallFolderNotFound = errors.New("call folder not found")
var ErrInvalidCallFolderInput = errors.New("invalid call folder input")
var ErrCallFolderScopeMismatch = errors.New("call folder scope mismatch")
var ErrInvalidDeepAnalysisInput = errors.New("invalid deep analysis input")
var ErrAggregateAnalysisNotFound = errors.New("aggregate analysis not found")
var ErrNoAnalyzedCallsForDeepAnalysis = errors.New("no analyzed calls for deep analysis")
var ErrDeepAnalysisLimitExceeded = errors.New("deep analysis limit exceeded")

// AUDIO
var ErrAudioFileNotFound = errors.New("audio file not found")
var ErrInvalidAudioPath = errors.New("invalid audio path")
var ErrAudioDurationDetect = errors.New("audio duration detect failed")
var ErrAudioProbeNotFound = errors.New("audio metadata probe not found")
var ErrAudioFileUnreadable = errors.New("audio file unreadable")

// USER
var ErrUserNotFound = errors.New("user not found")
var ErrUserAlreadyExists = errors.New("user already exists")
var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrInvalidUserInput = errors.New("invalid user input")
var ErrInvalidContactInput = errors.New("invalid contact input")

// ADMIN
var ErrInvalidAdminInput = errors.New("invalid admin input")
var ErrInvalidUserRole = errors.New("invalid user role")
var ErrAdminReasonRequired = errors.New("admin reason is required")
var ErrRoleTransitionForbidden = errors.New("role transition is forbidden")
var ErrProtectedSuperAdmin = errors.New("superadmin is protected")
var ErrCannotChangeOwnRole = errors.New("cannot change own role")
var ErrUserRoleChanged = errors.New("user role changed")
var ErrAdminSessionManagementForbidden = errors.New("admin session management is forbidden")

// COMPANY
var ErrCompanyNotFound = errors.New("company not found")
var ErrInvalidCompanyInput = errors.New("invalid company input")
var ErrCompanyTagAlreadyExists = errors.New("company tag already exists")
var ErrUserAlreadyManagesCompany = errors.New("user already manages company")
var ErrLastCompanyManager = errors.New("last company manager cannot be removed")
var ErrCompanyMembershipConflict = errors.New("user already belongs to another company")
var ErrCompanyNotEmpty = errors.New("company still has members")
var ErrCompanyDeputyAlreadyAssigned = errors.New("company already has a deputy")
var ErrCompanyDeputyNotAssigned = errors.New("company has no deputy")
var ErrOwnerOnlyAction = errors.New("action is available to the company owner only")
var ErrCompanyOwnershipTransferNotFound = errors.New("company ownership transfer not found")
var ErrCompanyOwnershipTransferPending = errors.New("company ownership transfer is already pending")
var ErrDepartmentTransferNotFound = errors.New("department transfer request not found")
var ErrDepartmentTransferPending = errors.New("department transfer request is already pending")
var ErrMembershipRestricted = errors.New("membership is restricted and needs approval")
var ErrInvitationsMuted = errors.New("user does not accept invitations")
var ErrTargetAlreadyEngaged = errors.New("user already belongs to a company or department")
var ErrDepartmentTransferRequired = errors.New("moving this person between departments requires a transfer request")

// DepartmentTransferRequired tells the interface which colleague a leader has to
// request instead of inviting.
type DepartmentTransferRequired struct {
	UserUUID uuid.UUID
}

func (e *DepartmentTransferRequired) Error() string {
	return ErrDepartmentTransferRequired.Error()
}

func (e *DepartmentTransferRequired) Unwrap() error {
	return ErrDepartmentTransferRequired
}

// CompanyMembershipConflict carries the company a user is about to leave, so the
// interface can name it in the confirmation alert instead of showing a bare
// error.
type CompanyMembershipConflict struct {
	CurrentCompanyUUID uuid.UUID
	CurrentCompanyName string
}

func (e *CompanyMembershipConflict) Error() string {
	return ErrCompanyMembershipConflict.Error()
}

func (e *CompanyMembershipConflict) Unwrap() error {
	return ErrCompanyMembershipConflict
}

var ErrInvitationApprovalRequired = errors.New("invitation needs approval")

// DEPARTMENT
var ErrDepartmentNotFound = errors.New("department not found")
var ErrInvalidDepartmentInput = errors.New("invalid department input")
var ErrForbidden = errors.New("forbidden")

// REFRESH SESSION
var ErrRefreshSessionNotFound = errors.New("refresh session not found")
var ErrInvalidRefreshToken = errors.New("invalid refresh token")
var ErrRefreshRotationConflict = errors.New("refresh rotation already completed")
var ErrRefreshTokenReuse = errors.New("refresh token reuse detected")
var ErrSessionNotTrusted = errors.New("session is not trusted yet")

// TRANSCRIPT
var ErrTranscriptionNotFound = errors.New("transcription not found")
var ErrInvalidTranscriptionInput = errors.New("invalid transcription input")
var ErrInvalidTranscriptionEdit = errors.New("invalid transcription edit")
var ErrNoTranscriptionChanges = errors.New("no transcription changes")
var ErrTranscriptionRevisionConflict = errors.New("transcription revision conflict")
var ErrTranscriptionEditForbidden = errors.New("transcription edit forbidden")
var ErrRedactedWordEditForbidden = errors.New("redacted word edit forbidden")
var ErrTranscriptionNotEditable = errors.New("transcription not editable")
var ErrTranscriptionLockedByReview = errors.New("transcription locked by quality review")
var ErrNoCallsForProcessing = errors.New("no calls for processing")

// TRANSCRIBER
var ErrTranscriberNotConfigured = errors.New("transcriber not configured")

// ANALYSIS
var ErrAnalysisNotFound = errors.New("analysis not found")
var ErrInvalidAnalysisInput = errors.New("invalid analysis input")
var ErrAnalyzerNotConfigured = errors.New("analyzer not configured")
var ErrInvalidAnalysisStatus = errors.New("invalid analysis status")
var ErrAnalysisSuperseded = errors.New("analysis superseded by newer transcription")
var ErrTestCallReadOnly = errors.New("test call is read only")

// PROCESSING JOB
var ErrProcessingJobNotFound = errors.New("processing job not found")
var ErrNoProcessingJobs = errors.New("no processing jobs")
var ErrInvalidProcessingJobType = errors.New("invalid processing job type")

// ANALYSIS INSTRUCTION
var ErrAnalysisInstructionNotFound = errors.New("analysis instruction not found")
var ErrInvalidAnalysisInstructionInput = errors.New("invalid analysis instruction input")
var ErrUnsupportedInstructionType = errors.New("unsupported instruction type")
var ErrInstructionFileNotFound = errors.New("instruction file not found")
var ErrInvalidInstructionPath = errors.New("invalid instruction path")
var ErrInstructionLimitExceeded = errors.New("instruction limit exceeded")

// BILLING
var ErrPlanNotFound = errors.New("plan not found")
var ErrInvalidBillingInput = errors.New("invalid billing input")
var ErrInvalidAPIKey = errors.New("invalid api key")
var ErrAPIKeyEnvironmentMismatch = errors.New("api key environment mismatch")
var ErrAPIKeyScopeDenied = errors.New("api key scope denied")
var ErrSubscriptionNotFound = errors.New("subscription not found")
var ErrSubscriptionRequired = errors.New("subscription required")
var ErrPlanLimitExceeded = errors.New("plan limit exceeded")
var ErrMonthlyMinutesLimitExceeded = errors.New("monthly minutes limit exceeded")
var ErrInsufficientCredits = errors.New("insufficient credits")
var ErrCreditOperationNotFound = errors.New("credit operation not found")
var ErrCreditOperationConflict = errors.New("credit operation conflict")
var ErrApplicationBudgetExceeded = errors.New("application credit budget exceeded")
var ErrIntegrationNotFound = errors.New("integration not found")
var ErrIntegrationConflict = errors.New("integration conflict")
var ErrIntegrationDisabled = errors.New("integration disabled")
var ErrRecordingURLForbidden = errors.New("recording url forbidden")
var ErrIngestNotFound = errors.New("ingest item not found")
var ErrCompanyLimitExceeded = errors.New("company limit exceeded")
var ErrDepartmentLimitExceeded = errors.New("department limit exceeded")
var ErrMemberLimitExceeded = errors.New("member limit exceeded")
var ErrExportAccessDenied = errors.New("export access denied")
var ErrTeamAnalyticsAccessDenied = errors.New("team analytics access denied")
var ErrAPIAccessDenied = errors.New("api access denied")

// REPORT
var ErrReportNotFound = errors.New("report not found")
var ErrReportAlreadyExists = errors.New("report already exists")
var ErrInvalidReportInput = errors.New("invalid report input")
var ErrUnsupportedReportFormat = errors.New("unsupported report format")
var ErrUnsupportedReportScope = errors.New("unsupported report scope")
var ErrReportScopeNotImplemented = errors.New("report scope not implemented")
var ErrReportFileNotFound = errors.New("report file not found")
var ErrInvalidReportPath = errors.New("invalid report path")
var ErrReportNotReady = errors.New("report not ready")
var ErrReportExpired = errors.New("report expired")
var ErrAggregateReportNotFound = errors.New("aggregate report not found")
var ErrInvalidAggregateReportInput = errors.New("invalid aggregate report input")
var ErrAggregateReportFileNotFound = errors.New("aggregate report file not found")

// INVITATION
var ErrInvitationNotFound = errors.New("invitation not found")
var ErrInvalidInvitationInput = errors.New("invalid invitation input")
var ErrInvitationAlreadyExists = errors.New("invitation already exists")
var ErrInvitationNotPending = errors.New("invitation not pending")
var ErrInvitationExpired = errors.New("invitation expired")
var ErrInvitationConvert = errors.New("invitation convert error")

// SEARCH
var ErrInvalidSearchInput = errors.New("invalid search input")

// NOTIFICATION
var ErrNotificationNotFound = errors.New("notification not found")
var ErrInvalidNotificationInput = errors.New("invalid notification input")
var ErrDepartmentMembershipConflict = errors.New("user already has an active department; explicit transfer required")
