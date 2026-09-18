// Package analyticsfacts keeps the analytics facts of calls: a projection of
// each call's effective analysis, with the reviewer's scores beside the
// model's, so that analytics never reads result_json.
package analyticsfacts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Service struct {
	db  *sql.DB
	log logger.Logger
}

func NewService(db *sql.DB, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}
	return &Service{db: db, log: log}
}

// Refresh projects a call and logs a failure: a projection must never fail the
// analysis, the review or the edit that asked for it; the worker catches up.
func (s *Service) Refresh(ctx context.Context, callID uuid.UUID) {
	if err := s.Project(ctx, callID); err != nil {
		s.log.Warn(ctx, "analytics facts not projected", zap.String("call_id", callID.String()), zap.Error(err))
	}
}

// Project replaces the facts of one call with what its effective analysis says
// now. It is idempotent and serialised on the call row, since analysis, review,
// subject changes and the worker all call it.
func (s *Service) Project(ctx context.Context, callID uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin projection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var occurredAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(occurred_at, created_at) FROM calls WHERE call_uuid = $1 FOR UPDATE`, callID).Scan(&occurredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock call: %w", err)
	}
	var analysisID uuid.UUID
	var status, model string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT analysis_uuid, status, COALESCE(result_json, '{}'::jsonb), COALESCE(model, '') FROM call_analyses WHERE call_uuid = $1`, callID).
		Scan(&analysisID, &status, &raw, &model)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `DELETE FROM analytics_call_facts WHERE call_uuid = $1`, callID); err != nil {
			return fmt.Errorf("clear facts: %w", err)
		}
		return tx.Commit()
	case err != nil:
		return fmt.Errorf("read analysis: %w", err)
	}
	if status != string(models.CallAnalysisStatusDone) {
		// A stale or re-running analysis keeps the last facts that were true;
		// the call page shows the same result.
		return tx.Commit()
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("parse analysis result: %w", err)
	}
	human, err := loadHuman(ctx, tx, analysisID)
	if err != nil {
		return err
	}
	var shared, internal bool
	err = tx.QueryRowContext(ctx, `SELECT is_shared, is_internal FROM call_subject_states WHERE call_uuid = $1`, callID).Scan(&shared, &internal)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read call subject state: %w", err)
	}

	projection := project(result, human)
	projection.call.AnalysisID, projection.call.OccurredAt, projection.call.JudgeModel = analysisID, occurredAt, model
	projection.call.IsShared, projection.call.IsInternal = shared, internal
	if err := s.resolveCriteria(ctx, tx, &projection); err != nil {
		return err
	}
	if err := write(ctx, tx, callID, projection); err != nil {
		return err
	}
	return tx.Commit()
}

type callFact struct {
	AnalysisID            uuid.UUID
	OccurredAt            time.Time
	IsShared              bool
	IsInternal            bool
	SchemaVersion         int
	ScorecardMode         string
	PipelineVersion       string
	JudgeModel            string
	AIOverall             *int
	HumanOverall          *int
	CriteriaScore         *int
	UnassessedWeightShare float64
	CoverageStatus        string
	CriteriaTotal         int
	CriteriaScored        int
	CriticalMissed        int
	QuestionsTotal        int
	QuestionsAvg          *int
}

type criterionFact struct {
	Key            uuid.UUID
	InstructionID  uuid.UUID
	ScorecardID    uuid.UUID
	ItemID         string
	AIStatus       string
	AIScore        *int
	HumanScore     *int
	HumanDecision  *string
	Weight         int
	IsCritical     bool
	CountedInScore bool
	EvidenceStart  *float64
}

type projection struct {
	call     callFact
	criteria []criterionFact
	// also are keys of other instructions scored under a criterion of this
	// call; their instruction and scorecard are looked up.
	also []criterionFact
}

// humanReview is the published human decision on an analysis.
type humanReview struct {
	Overall  *float64
	Criteria map[string]humanCriterion
}

type humanCriterion struct {
	Score    *float64
	Decision string
}

func loadHuman(ctx context.Context, tx *sql.Tx, analysisID uuid.UUID) (humanReview, error) {
	review := humanReview{Criteria: map[string]humanCriterion{}}
	var revisionID uuid.UUID
	var score, scoreMax sql.NullFloat64
	err := tx.QueryRowContext(ctx, `
		SELECT r.revision_uuid, r.human_score::float8, r.score_max::float8
		FROM call_quality_reviews q
		JOIN call_quality_review_revisions r ON r.revision_uuid = q.active_revision_uuid
		WHERE q.analysis_uuid = $1 AND q.status <> 'canceled' AND r.status = 'published'`, analysisID).Scan(&revisionID, &score, &scoreMax)
	if errors.Is(err, sql.ErrNoRows) {
		return review, nil
	}
	if err != nil {
		return review, fmt.Errorf("read human review: %w", err)
	}
	if score.Valid && scoreMax.Valid && scoreMax.Float64 > 0 {
		value := score.Float64 / scoreMax.Float64 * 100
		review.Overall = &value
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT criterion_key, human_score::float8, score_min::float8, score_max::float8, decision
		FROM call_quality_review_criteria WHERE revision_uuid = $1`, revisionID)
	if err != nil {
		return review, fmt.Errorf("read human criteria: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key, decision string
		var value, low, high sql.NullFloat64
		if err := rows.Scan(&key, &value, &low, &high, &decision); err != nil {
			return review, fmt.Errorf("scan human criterion: %w", err)
		}
		item := humanCriterion{Decision: decision}
		if value.Valid {
			min, max := 0.0, 100.0
			if low.Valid {
				min = low.Float64
			}
			if high.Valid {
				max = high.Float64
			}
			if max > min {
				normalized := (value.Float64 - min) / (max - min) * 100
				item.Score = &normalized
			}
		}
		review.Criteria[key] = item
	}
	return review, rows.Err()
}

// Statuses that are not a score: they leave the formula, and all but
// not_applicable count as weight that could not be assessed.
var unassessedStatuses = map[string]bool{"unclear": true, "conflict": true, "not_assessed": true}

// criticalMissBelow is the effective score under which a critical criterion
// counts as missed: the model's "missed" and the lowest human band.
const criticalMissBelow = 13

// project turns an analysis result into facts. It is pure: loading and writing
// are done around it.
func project(result map[string]any, human humanReview) projection {
	var p projection
	version := int(number(result["schema_version"]))
	p.call.SchemaVersion = version
	p.call.PipelineVersion = text(result["pipeline_version"])
	p.call.HumanOverall = roundPtr(human.Overall)
	if version != 3 {
		p.call.ScorecardMode = "legacy"
		p.call.CoverageStatus = "complete"
		if score, ok := legacyScore(result); ok {
			p.call.AIOverall = roundPtr(&score)
		}
		return p
	}
	p.call.CoverageStatus = text(object(result["coverage"])["status"])
	if p.call.CoverageStatus == "" {
		p.call.CoverageStatus = "partial"
	}
	if score, ok := result["overall_score"].(float64); ok {
		p.call.AIOverall = roundPtr(&score)
	}
	var totalWeight, unassessedWeight, criteriaWeighted, criteriaWeight, questionsSum float64
	var questionsScored int
	requirements := 0
	for _, raw := range array(result["items"]) {
		item := object(raw)
		status := text(item["status"])
		weight := number(item["weight"])
		if weight <= 0 {
			weight = 1
		}
		id := text(item["id"])
		decision, reviewed := human.Criteria[id]
		notApplicable := status == "not_applicable" || (reviewed && decision.Decision == "not_applicable")
		if !notApplicable {
			totalWeight += weight
			if unassessedStatuses[status] && (!reviewed || decision.Score == nil) {
				unassessedWeight += weight
			}
		}
		aiScore := scoreOf(item)
		effective := aiScore
		if reviewed && (decision.Decision == "confirmed" || decision.Decision == "overridden") && decision.Score != nil {
			effective = decision.Score
		}
		if notApplicable {
			effective = nil
		}
		switch text(item["kind"]) {
		case "question":
			p.call.QuestionsTotal++
			if effective != nil {
				questionsSum += *effective
				questionsScored++
			}
		case "requirement":
			requirements++
			key, err := uuid.Parse(text(item["criterion_key"]))
			if err != nil {
				continue
			}
			fact := criterionFact{
				Key: key, ItemID: id, AIStatus: status, AIScore: roundPtr(aiScore),
				Weight: int(weight), IsCritical: item["is_critical"] == true, CountedInScore: true,
			}
			fact.InstructionID, _ = uuid.Parse(first(array(item["instruction_sources"])))
			fact.ScorecardID, _ = uuid.Parse(text(item["scorecard_uuid"]))
			if reviewed && decision.Decision != "unscored" {
				d := decision.Decision
				fact.HumanDecision = &d
				if d == "confirmed" || d == "overridden" {
					fact.HumanScore = roundPtr(decision.Score)
				}
			}
			if evidence := array(item["evidence"]); len(evidence) > 0 {
				if start, ok := object(evidence[0])["start_seconds"].(float64); ok {
					fact.EvidenceStart = &start
				}
			}
			p.criteria = append(p.criteria, fact)
			p.call.CriteriaTotal++
			if effective != nil {
				p.call.CriteriaScored++
				criteriaWeighted += *effective * weight
				criteriaWeight += weight
				if fact.IsCritical && *effective < criticalMissBelow {
					p.call.CriticalMissed++
				}
			}
			for _, raw := range array(item["also_criterion_keys"]) {
				alsoKey, err := uuid.Parse(text(raw))
				if err != nil {
					continue
				}
				also := fact
				also.Key, also.CountedInScore, also.InstructionID, also.ScorecardID = alsoKey, false, uuid.Nil, uuid.Nil
				p.also = append(p.also, also)
			}
		}
	}
	if criteriaWeight > 0 {
		score := criteriaWeighted / criteriaWeight
		p.call.CriteriaScore = roundPtr(&score)
	}
	if questionsScored > 0 {
		avg := questionsSum / float64(questionsScored)
		p.call.QuestionsAvg = roundPtr(&avg)
	}
	if totalWeight > 0 {
		p.call.UnassessedWeightShare = math.Round(unassessedWeight/totalWeight*100) / 100
	}
	p.call.ScorecardMode = text(result["scorecard_mode"])
	switch p.call.ScorecardMode {
	case "fixed", "partial", "adhoc", "none":
	default:
		// Results from before scorecards had requirements without keys.
		p.call.ScorecardMode = "none"
		if requirements > 0 {
			p.call.ScorecardMode = "adhoc"
		}
	}
	return p
}

// resolveCriteria brings every key to its canonical form and finds the
// instruction and scorecard of keys that rode along on another criterion.
func (s *Service) resolveCriteria(ctx context.Context, tx *sql.Tx, p *projection) error {
	keys := make([]string, 0, len(p.criteria)+len(p.also))
	for _, c := range append(append([]criterionFact{}, p.criteria...), p.also...) {
		keys = append(keys, c.Key.String())
	}
	if len(keys) == 0 {
		return nil
	}
	canonical := map[uuid.UUID]uuid.UUID{}
	rows, err := tx.QueryContext(ctx, `SELECT alias_key, canonical_key FROM criterion_key_aliases WHERE alias_key = ANY($1::uuid[])`, keys)
	if err != nil {
		return fmt.Errorf("read criterion aliases: %w", err)
	}
	for rows.Next() {
		var alias, target uuid.UUID
		if err := rows.Scan(&alias, &target); err != nil {
			_ = rows.Close()
			return err
		}
		canonical[alias] = target
	}
	_ = rows.Close()
	type origin struct{ instruction, scorecard uuid.UUID }
	origins := map[uuid.UUID]origin{}
	rows, err = tx.QueryContext(ctx, `
		SELECT DISTINCT ON (c.criterion_key) c.criterion_key, s.instruction_uuid, s.scorecard_uuid
		FROM instruction_scorecard_criteria c JOIN instruction_scorecards s ON s.scorecard_uuid = c.scorecard_uuid
		WHERE c.criterion_key = ANY($1::uuid[])
		ORDER BY c.criterion_key, s.is_current DESC, s.created_at DESC`, keys)
	if err != nil {
		return fmt.Errorf("read criterion origins: %w", err)
	}
	for rows.Next() {
		var key uuid.UUID
		var o origin
		if err := rows.Scan(&key, &o.instruction, &o.scorecard); err != nil {
			_ = rows.Close()
			return err
		}
		origins[key] = o
	}
	_ = rows.Close()

	seen := map[uuid.UUID]bool{}
	var merged []criterionFact
	for _, list := range [][]criterionFact{p.criteria, p.also} {
		for _, c := range list {
			if o, ok := origins[c.Key]; ok {
				if c.InstructionID == uuid.Nil {
					c.InstructionID = o.instruction
				}
				if c.ScorecardID == uuid.Nil {
					c.ScorecardID = o.scorecard
				}
			}
			if target, ok := canonical[c.Key]; ok {
				c.Key = target
			}
			if seen[c.Key] || c.InstructionID == uuid.Nil || c.ScorecardID == uuid.Nil {
				continue
			}
			seen[c.Key] = true
			merged = append(merged, c)
		}
	}
	p.criteria, p.also = merged, nil
	return nil
}

func write(ctx context.Context, tx *sql.Tx, callID uuid.UUID, p projection) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM analytics_call_facts WHERE call_uuid = $1`, callID); err != nil {
		return fmt.Errorf("clear facts: %w", err)
	}
	c := p.call
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO analytics_call_facts (
			call_uuid, analysis_uuid, occurred_at, is_shared, is_internal, schema_version, scorecard_mode, pipeline_version, judge_model,
			ai_overall_score, human_overall_score, criteria_score, unassessed_weight_share, coverage_status,
			criteria_total, criteria_scored, critical_missed, questions_total, questions_avg_score, projected_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,now())`,
		callID, c.AnalysisID, c.OccurredAt, c.IsShared, c.IsInternal, c.SchemaVersion, c.ScorecardMode, c.PipelineVersion, c.JudgeModel,
		c.AIOverall, c.HumanOverall, c.CriteriaScore, c.UnassessedWeightShare, c.CoverageStatus,
		c.CriteriaTotal, c.CriteriaScored, c.CriticalMissed, c.QuestionsTotal, c.QuestionsAvg); err != nil {
		return fmt.Errorf("insert call fact: %w", err)
	}
	for _, f := range p.criteria {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO analytics_criterion_facts (
				call_uuid, criterion_key, instruction_uuid, scorecard_uuid, item_id, ai_status, ai_score, human_score, human_decision,
				weight, is_critical, counted_in_score, evidence_start_seconds
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			callID, f.Key, f.InstructionID, f.ScorecardID, f.ItemID, f.AIStatus, f.AIScore, f.HumanScore, f.HumanDecision,
			f.Weight, f.IsCritical, f.CountedInScore, f.EvidenceStart); err != nil {
			return fmt.Errorf("insert criterion fact: %w", err)
		}
	}
	return nil
}

// Rekey moves stored facts from a key to the one it was tied to, inside the
// transaction that records the alias. A call is scored against one version, so
// the two keys never meet in one call; if they do, the canonical row wins.
func Rekey(ctx context.Context, tx *sql.Tx, aliasKey, canonicalKey uuid.UUID) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE analytics_criterion_facts f SET criterion_key = $2
		WHERE f.criterion_key = $1
		  AND NOT EXISTS (SELECT 1 FROM analytics_criterion_facts o WHERE o.call_uuid = f.call_uuid AND o.criterion_key = $2)`, aliasKey, canonicalKey); err != nil {
		return fmt.Errorf("rekey criterion facts: %w", err)
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM analytics_criterion_facts WHERE criterion_key = $1`, aliasKey)
	return err
}

func scoreOf(item map[string]any) *float64 {
	if unassessedStatuses[text(item["status"])] || text(item["status"]) == "not_applicable" {
		return nil
	}
	if score, ok := item["score"].(float64); ok {
		return &score
	}
	return nil
}

// legacyScore reads the overall score of a pre-v3 result the way the overview
// always has.
func legacyScore(payload map[string]any) (float64, bool) {
	if score, ok := payload["score"].(float64); ok && score >= 0 {
		if scale, ok := payload["score_scale"].(float64); ok && scale > 0 {
			return clamp(score / scale * 100), true
		}
	}
	for _, key := range []string{"quality_score", "overall_score", "manager_score", "score"} {
		score, ok := payload[key].(float64)
		if !ok || score < 0 {
			continue
		}
		if score > 5 {
			return clamp(score), true
		}
		return clamp(score * 20), true
	}
	return 0, false
}

func clamp(v float64) float64 { return math.Max(0, math.Min(100, v)) }

func roundPtr(v *float64) *int {
	if v == nil {
		return nil
	}
	r := int(math.Round(clamp(*v)))
	return &r
}

func number(v any) float64 {
	n, _ := v.(float64)
	return n
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func array(v any) []any {
	a, _ := v.([]any)
	return a
}

func first(values []any) string {
	if len(values) == 0 {
		return ""
	}
	return text(values[0])
}
