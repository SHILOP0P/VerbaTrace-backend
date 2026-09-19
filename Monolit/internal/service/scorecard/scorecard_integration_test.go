//go:build integration

package scorecard

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"verbatrace/monolit/internal/analyzer/mockstaged"
	"verbatrace/monolit/internal/analyzer/scorecardflow"
	"verbatrace/monolit/internal/models"
	instructionRepo "verbatrace/monolit/internal/repository/analysis_instruction"
	"verbatrace/monolit/internal/repository/repositorytest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type openAccess struct{ db *sql.DB }

func (a openAccess) AuthorizeRead(ctx context.Context, id, _ uuid.UUID) (models.AnalysisInstruction, error) {
	return instructionRepo.NewRepository(a.db).GetByUUIDIncludingInactive(ctx, id)
}

func (a openAccess) AuthorizeEdit(ctx context.Context, id, user uuid.UUID) (models.AnalysisInstruction, error) {
	return a.AuthorizeRead(ctx, id, user)
}

type countingAnalyzer struct {
	*mockstaged.Analyzer
	calls atomic.Int32
}

func (c *countingAnalyzer) Analyze(ctx context.Context, request models.AnalysisRequest) (models.AnalysisResult, error) {
	c.calls.Add(1)
	return c.Analyzer.Analyze(ctx, request)
}

type fixture struct {
	t        *testing.T
	db       *sql.DB
	service  *Service
	analyzer *countingAnalyzer
	repo     *instructionRepo.Repository
	userID   uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	db := repositorytest.OpenTestDB(t)
	repositorytest.RunMigrations(t, db)
	repositorytest.TruncateTables(t, db)
	provider := &countingAnalyzer{Analyzer: mockstaged.New("")}
	service := NewService(db, openAccess{db: db}, provider, nil, nil)
	service.planWait = 3 * time.Second
	service.planPoll = 50 * time.Millisecond
	return &fixture{t: t, db: db, service: service, analyzer: provider, repo: instructionRepo.NewRepository(db), userID: repositorytest.CreateUser(t, db)}
}

// createInstruction saves an instruction and gives its version a text, the way
// the analysis caches it, so no file storage is needed.
func (f *fixture) createInstruction(title, text string) models.AnalysisInstruction {
	now := time.Now().UTC()
	instruction, err := f.repo.Create(context.Background(), models.AnalysisInstruction{
		ID: uuid.New(), Scope: models.AnalysisInstructionScopePersonal, UserUUID: uuid.NullUUID{UUID: f.userID, Valid: true},
		Title: title, OriginalFilename: "rubric.md", FilePath: "p/" + uuid.NewString() + ".md", MimeType: "text/markdown",
		SizeBytes: int64(len(text)), ContentSHA256: hash(text), IsActive: true, CreatedByUserUUID: f.userID, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(f.t, err)
	f.setLatestText(instruction.ID, text)
	return instruction
}

func (f *fixture) saveText(instructionID uuid.UUID, text string) {
	_, err := f.db.Exec(`UPDATE analysis_instructions SET file_path = $2, content_sha256 = $3 WHERE instruction_uuid = $1`, instructionID, "p/"+uuid.NewString()+".md", hash(text))
	require.NoError(f.t, err)
	f.setLatestText(instructionID, text)
}

func (f *fixture) setLatestText(instructionID uuid.UUID, text string) {
	_, err := f.db.Exec(`UPDATE analysis_instruction_versions SET content_text = $2 WHERE instruction_version_uuid = (
		SELECT instruction_version_uuid FROM analysis_instruction_versions WHERE instruction_uuid = $1 ORDER BY version DESC LIMIT 1)`, instructionID, text)
	require.NoError(f.t, err)
}

// compileNow skips the quiet period and runs the worker once.
func (f *fixture) compileNow() {
	_, err := f.db.Exec(`UPDATE instruction_scorecards SET compile_after = now() WHERE status = 'queued'`)
	require.NoError(f.t, err)
	_, err = f.service.CompileDue(context.Background(), 10)
	require.NoError(f.t, err)
}

func (f *fixture) get(instructionID uuid.UUID) models.Scorecard {
	card, err := f.service.Get(context.Background(), instructionID, f.userID)
	require.NoError(f.t, err)
	return card
}

func hash(text string) string { return uuid.NewSHA1(uuid.NameSpaceOID, []byte(text)).String() }

func titles(card models.Scorecard) []string {
	result := make([]string, 0, len(card.Criteria))
	for _, c := range card.Criteria {
		result = append(result, c.Title)
	}
	return result
}

func keyOf(t *testing.T, card models.Scorecard, title string) uuid.UUID {
	t.Helper()
	for _, c := range card.Criteria {
		if c.Title == title {
			return c.Key
		}
	}
	t.Fatalf("criterion %q not found in %v", title, titles(card))
	return uuid.Nil
}

func TestSavingQueuesACompileAfterAQuietPeriodAndDropsDraftCompiles(t *testing.T) {
	f := newFixture(t)
	instruction := f.createInstruction("Стандарт", "- Выяснил бюджет клиента")

	var compileAfter time.Time
	require.NoError(t, f.db.QueryRow(`SELECT compile_after FROM instruction_scorecards WHERE instruction_uuid = $1`, instruction.ID).Scan(&compileAfter))
	require.WithinDuration(t, time.Now().Add(2*time.Minute), compileAfter, 30*time.Second)

	// Nothing is compiled before the quiet period ends.
	taken, err := f.service.CompileDue(context.Background(), 10)
	require.NoError(t, err)
	require.Zero(t, taken)

	// Ten saves in a row leave one queued compile, of the last version.
	for i := 0; i < 10; i++ {
		f.saveText(instruction.ID, "- Выяснил бюджет клиента\n- Черновик "+strings.Repeat("!", i+1))
	}
	var queued int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM instruction_scorecards WHERE instruction_uuid = $1`, instruction.ID).Scan(&queued))
	require.Equal(t, 1, queued)
	f.compileNow()
	require.Equal(t, int32(1), f.analyzer.calls.Load())
	require.Equal(t, models.ScorecardStatusReady, f.get(instruction.ID).Status)
}

func TestCompileKeepsKeysAcrossVersionsAndCopiesARename(t *testing.T) {
	f := newFixture(t)
	instruction := f.createInstruction("Стандарт", "Менеджер должен:\n- Выяснил бюджет клиента\n- Назвал следующий шаг [[critical]]\n- Представился [[weight:3]]")
	f.compileNow()
	first := f.get(instruction.ID)
	require.Equal(t, models.ScorecardStatusReady, first.Status)
	require.True(t, first.IsCurrent)
	require.Equal(t, []string{"Выяснил бюджет клиента", "Назвал следующий шаг", "Представился"}, titles(first))
	require.True(t, first.Criteria[1].IsCritical)
	require.Equal(t, 3, first.Criteria[2].Weight)
	for _, c := range first.Criteria {
		require.Equal(t, models.CriterionChangeNew, c.ChangeKind)
	}

	// One criterion changes, one is removed, one is added.
	f.saveText(instruction.ID, "Менеджер должен:\n- Выяснил бюджет клиента\n- Назвал следующий шаг [[critical]]\n- Уточнил сроки")
	f.compileNow()
	second := f.get(instruction.ID)
	require.Equal(t, keyOf(t, first, "Выяснил бюджет клиента"), keyOf(t, second, "Выяснил бюджет клиента"))
	require.Equal(t, keyOf(t, first, "Назвал следующий шаг"), keyOf(t, second, "Назвал следующий шаг"))
	require.Equal(t, models.CriterionChangeUnchanged, second.Criteria[0].ChangeKind)
	require.Equal(t, models.CriterionChangeNew, second.Criteria[2].ChangeKind)
	require.Len(t, second.RemovedCriteria, 1)
	require.Equal(t, "Представился", second.RemovedCriteria[0].Title)

	// Old scorecard stays with its version; only the new one is in force.
	var current int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM instruction_scorecards WHERE instruction_uuid = $1 AND is_current`, instruction.ID).Scan(&current))
	require.Equal(t, 1, current)

	// A rename changes no text: the scorecard is copied without a model call.
	calls := f.analyzer.calls.Load()
	_, err := f.db.Exec(`UPDATE analysis_instructions SET title = 'Стандарт 2' WHERE instruction_uuid = $1`, instruction.ID)
	require.NoError(t, err)
	f.compileNow()
	renamed := f.get(instruction.ID)
	require.Equal(t, calls, f.analyzer.calls.Load())
	require.Equal(t, models.ScorecardOriginCopied, renamed.Origin)
	require.Equal(t, keyOf(t, second, "Уточнил сроки"), keyOf(t, renamed, "Уточнил сроки"))
}

func TestOwnerCorrectsTheAutomaticMatchBothWays(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	instruction := f.createInstruction("Стандарт", "- Выяснил бюджет клиента\n- Представился")
	f.compileNow()
	first := f.get(instruction.ID)
	f.saveText(instruction.ID, "- Выяснил бюджет клиента\n- Назвал своё имя и компанию")
	f.compileNow()
	second := f.get(instruction.ID)
	renamed := keyOf(t, second, "Назвал своё имя и компанию")
	introduced := keyOf(t, first, "Представился")
	require.Len(t, second.RemovedCriteria, 1)

	linked, err := f.service.SameAs(ctx, instruction.ID, f.userID, renamed, introduced)
	require.NoError(t, err)
	require.Empty(t, linked.RemovedCriteria, "the removed criterion lives on under the new key")
	require.NotNil(t, linked.Criteria[1].SameAs)
	require.Equal(t, "Представился", linked.Criteria[1].SameAs.Title)
	_, err = f.service.SameAs(ctx, instruction.ID, f.userID, renamed, introduced)
	require.ErrorIs(t, err, models.ErrScorecardInvalid, "a criterion is tied to one predecessor")

	budget := keyOf(t, second, "Выяснил бюджет клиента")
	split, err := f.service.Split(ctx, instruction.ID, f.userID, budget)
	require.NoError(t, err)
	require.Equal(t, second.Revision+1, split.Revision)
	require.NotEqual(t, budget, split.Criteria[0].Key)
	require.Equal(t, models.CriterionChangeNew, split.Criteria[0].ChangeKind)
	require.Equal(t, []string{"Выяснил бюджет клиента"}, []string{split.RemovedCriteria[0].Title}, "the old criterion now ends")
	_, err = f.service.Split(ctx, instruction.ID, f.userID, split.Criteria[0].Key)
	require.ErrorIs(t, err, models.ErrScorecardInvalid)
}

func TestEditMakesARevisionAndCarriesOverToTheNextVersion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	instruction := f.createInstruction("Стандарт", "- Выяснил бюджет клиента\n- Назвал следующий шаг")
	f.compileNow()
	card := f.get(instruction.ID)
	budget := keyOf(t, card, "Выяснил бюджет клиента")

	weight, critical := 2, true
	edited, err := f.service.Edit(ctx, models.EditScorecardInput{InstructionID: instruction.ID, UserID: f.userID, LockVersion: card.LockVersion,
		Criteria: []models.ScorecardCriterionEdit{{Key: budget, Weight: &weight, IsCritical: &critical}}})
	require.NoError(t, err)
	require.Equal(t, 2, edited.Revision)
	require.True(t, edited.IsCurrent)
	require.Equal(t, models.ScorecardOriginEdited, edited.Origin)
	require.ElementsMatch(t, []string{"weight", "is_critical"}, edited.Criteria[0].EditedFields)

	// The revision a call was scored by keeps its values.
	original, err := loadCard(ctx, f.db, card.ID)
	require.NoError(t, err)
	require.Equal(t, 1, original.Criteria[0].Weight)
	require.False(t, original.IsCurrent)

	_, err = f.service.Edit(ctx, models.EditScorecardInput{InstructionID: instruction.ID, UserID: f.userID, LockVersion: card.LockVersion,
		Criteria: []models.ScorecardCriterionEdit{{Key: budget, Weight: &weight}}})
	require.ErrorIs(t, err, models.ErrScorecardVersionConflict)

	// The next version does not know the edit, yet keeps it.
	f.saveText(instruction.ID, "- Выяснил бюджет клиента\n- Назвал следующий шаг\n- Уточнил сроки")
	f.compileNow()
	next := f.get(instruction.ID)
	require.Equal(t, 2, next.Criteria[0].Weight)
	require.True(t, next.Criteria[0].IsCritical)
}

func TestEnabledLimitHoldsOnCompileAndOnEdit(t *testing.T) {
	f := newFixture(t)
	var lines []string
	for i := 1; i <= scorecardflow.MaxEnabled+2; i++ {
		lines = append(lines, "- Требование номер "+strings.Repeat("я", i))
	}
	instruction := f.createInstruction("Длинная", strings.Join(lines, "\n"))
	f.compileNow()
	card := f.get(instruction.ID)
	enabled := 0
	for _, c := range card.Criteria {
		if c.Enabled {
			enabled++
		}
	}
	require.Equal(t, scorecardflow.MaxEnabled, enabled)
	require.False(t, card.Criteria[len(card.Criteria)-1].Enabled)

	on := true
	_, err := f.service.Edit(context.Background(), models.EditScorecardInput{InstructionID: instruction.ID, UserID: f.userID, LockVersion: card.LockVersion,
		Criteria: []models.ScorecardCriterionEdit{{Key: card.Criteria[len(card.Criteria)-1].Key, Enabled: &on}}})
	require.ErrorIs(t, err, models.ErrScorecardLimit)
}

func TestConfirmationKeepsTheAppliedScorecardUntilTheNewOneIsApplied(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	instruction := f.createInstruction("Стандарт", "- Выяснил бюджет клиента")
	f.compileNow()
	applied := f.get(instruction.ID)

	on := true
	_, err := f.service.Edit(ctx, models.EditScorecardInput{InstructionID: instruction.ID, UserID: f.userID, ConfirmRequired: &on})
	require.NoError(t, err)
	f.saveText(instruction.ID, "- Выяснил бюджет клиента\n- Уточнил сроки")
	f.compileNow()
	waiting := f.get(instruction.ID)
	require.True(t, waiting.AwaitingConfirmation)
	require.False(t, waiting.IsCurrent)

	// Calls keep being analysed with the applied version, text and criteria.
	plan, err := f.service.PlanForAnalysis(ctx, []models.AnalysisInstructionContent{{ID: instruction.ID, Scope: instruction.Scope, Title: "Стандарт", Content: "новый текст", VersionID: waiting.VersionID}})
	require.NoError(t, err)
	require.Equal(t, models.ScorecardModeFixed, plan.Mode)
	require.Equal(t, applied.VersionID, plan.Instructions[0].VersionID)
	require.Equal(t, "- Выяснил бюджет клиента", plan.Instructions[0].Content)
	require.Len(t, plan.Requirements, 1)

	confirmed, err := f.service.Confirm(ctx, instruction.ID, *ptr(waiting.ID), f.userID, waiting.LockVersion)
	require.NoError(t, err)
	require.True(t, confirmed.IsCurrent)
	require.False(t, confirmed.AwaitingConfirmation)
}

func ptr[T any](value T) *T { return &value }

func TestPlanCompilesMissingScorecardsAndFallsBackWhenItCannot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	instruction := f.createInstruction("Стандарт", "- Выяснил бюджет клиента\n- Назвал следующий шаг")
	version := f.get(instruction.ID).VersionID

	// The analysis cannot wait for the worker here, so compile on its behalf.
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(200 * time.Millisecond)
		_, _ = f.service.CompileDue(ctx, 10)
	}()
	plan, err := f.service.PlanForAnalysis(ctx, []models.AnalysisInstructionContent{{ID: instruction.ID, Scope: instruction.Scope, Title: "Стандарт", VersionID: version}})
	<-done
	require.NoError(t, err)
	require.Equal(t, models.ScorecardModeFixed, plan.Mode)
	require.Len(t, plan.Requirements, 2)
	require.Equal(t, plan.Scorecards[0].ScorecardID, plan.Instructions[0].ScorecardID)

	// Prose without requirements fails to compile; the analysis breaks it down itself.
	prose := f.createInstruction("Справка", "Компания продаёт окна.")
	proseVersion := f.get(prose.ID).VersionID
	f.compileNow()
	require.Equal(t, models.ScorecardStatusFailed, f.get(prose.ID).Status)
	plan, err = f.service.PlanForAnalysis(ctx, []models.AnalysisInstructionContent{
		{ID: instruction.ID, Scope: instruction.Scope, VersionID: version},
		{ID: prose.ID, Scope: prose.Scope, VersionID: proseVersion},
	})
	require.NoError(t, err)
	require.Equal(t, models.ScorecardModePartial, plan.Mode)
	require.Len(t, plan.Adhoc, 1)
	require.Equal(t, prose.ID, plan.Adhoc[0].ID)
}

func TestPlanScoresADuplicatedRequirementOnceByTheNarrowerInstruction(t *testing.T) {
	company := planSlot{
		instruction: models.AnalysisInstructionContent{ID: uuid.New(), Title: "Компания"}, row: instructionRow{Scope: models.AnalysisInstructionScopeCompany},
		card: &models.Scorecard{ID: uuid.New(), Criteria: []models.ScorecardCriterion{
			{Key: uuid.New(), Position: 1, Title: "Поздоровался", Requirement: "Поздороваться", Weight: 1, Enabled: true},
			{Key: uuid.New(), Position: 2, Title: "Выяснил бюджет", Requirement: "Выяснить бюджет", Weight: 1, Enabled: true},
		}},
	}
	department := planSlot{
		instruction: models.AnalysisInstructionContent{ID: uuid.New(), Title: "Отдел"}, row: instructionRow{Scope: models.AnalysisInstructionScopeDepartment},
		card: &models.Scorecard{ID: uuid.New(), Criteria: []models.ScorecardCriterion{
			{Key: uuid.New(), Position: 1, Title: " поздоровался ", Requirement: "Поздороваться", Weight: 3, IsCritical: true, Enabled: true},
		}},
	}
	plan := buildPlan([]planSlot{company, department})
	require.Len(t, plan.Requirements, 2)
	var greeting models.AnalysisRequirement
	for _, requirement := range plan.Requirements {
		if requirement.Title == " поздоровался " {
			greeting = requirement
		}
	}
	require.Equal(t, 3, greeting.Weight, "the department's criterion wins")
	require.Equal(t, []uuid.UUID{company.card.Criteria[0].Key}, greeting.AlsoCriterionKeys)
}
