-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION effective_call_analysis_json(p_analysis_uuid UUID, p_result JSONB)
RETURNS JSONB
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
    v_revision_uuid UUID;
    v_revision_number INTEGER;
    v_total NUMERIC;
    v_source TEXT;
    v_criteria JSONB;
    v_custom JSONB;
BEGIN
    IF p_result IS NULL THEN
        RETURN p_result;
    END IF;

    SELECT q.active_revision_uuid, r.revision_number, r.human_score
      INTO v_revision_uuid, v_revision_number, v_total
      FROM call_quality_reviews q
      JOIN call_quality_review_revisions r ON r.revision_uuid = q.active_revision_uuid
     WHERE q.analysis_uuid = p_analysis_uuid
       AND q.status <> 'canceled'
       AND r.status = 'published'
     LIMIT 1;

    IF v_revision_uuid IS NULL THEN
        RETURN p_result;
    END IF;

    v_source := 'human_review_' || LEAST(v_revision_number, 2)::TEXT;

    SELECT COALESCE(jsonb_agg(
        CASE
            WHEN c.decision = 'not_applicable' THEN
                item || jsonb_build_object('status', 'not_applicable', 'points_awarded', NULL, 'score', NULL, 'effective_source', v_source)
            WHEN c.human_score IS NOT NULL THEN
                item || jsonb_build_object(
                    'points_awarded', c.human_score,
                    'points_max', c.score_max,
                    'score', CASE WHEN c.score_max > COALESCE(c.score_min, 0) THEN ((c.human_score - COALESCE(c.score_min, 0)) / (c.score_max - COALESCE(c.score_min, 0))) * 100 ELSE NULL END,
                    'effective_source', v_source
                )
            ELSE item || jsonb_build_object('effective_source', 'ai')
        END
        ORDER BY ordinal
    ), '[]'::jsonb)
      INTO v_criteria
      FROM jsonb_array_elements(COALESCE(p_result->'criteria_results', '[]'::jsonb)) WITH ORDINALITY AS source(item, ordinal)
      LEFT JOIN call_quality_review_criteria c
        ON c.revision_uuid = v_revision_uuid
       AND c.criterion_key = source.item->>'code';

    SELECT COALESCE(jsonb_agg(jsonb_build_object(
        'code', c.criterion_key,
        'title', c.title_snapshot,
        'topic', c.title_snapshot,
        'status', CASE WHEN c.decision = 'not_applicable' THEN 'not_applicable' ELSE 'met' END,
        'points_awarded', c.human_score,
        'points_max', c.score_max,
        'score', CASE WHEN c.human_score IS NOT NULL AND c.score_max > COALESCE(c.score_min, 0) THEN ((c.human_score - COALESCE(c.score_min, 0)) / (c.score_max - COALESCE(c.score_min, 0))) * 100 ELSE NULL END,
        'effective_source', v_source,
        'explanation', c.comment,
        'recommendation', '',
        'evidence_quotes', '[]'::jsonb,
        'evidence', '[]'::jsonb
    ) ORDER BY c.position), '[]'::jsonb)
      INTO v_custom
      FROM call_quality_review_criteria c
     WHERE c.revision_uuid = v_revision_uuid
       AND NOT EXISTS (
           SELECT 1
             FROM jsonb_array_elements(COALESCE(p_result->'criteria_results', '[]'::jsonb)) source
            WHERE source->>'code' = c.criterion_key
       );

    RETURN jsonb_set(
        jsonb_set(
            jsonb_set(p_result, '{criteria_results}', v_criteria || v_custom, true),
            '{effective_source}', to_jsonb(v_source), true
        ),
        '{score}', CASE WHEN v_total IS NULL THEN 'null'::jsonb ELSE to_jsonb(v_total) END, true
    ) || jsonb_build_object('score_scale', 100);
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS effective_call_analysis_json(UUID, JSONB);
