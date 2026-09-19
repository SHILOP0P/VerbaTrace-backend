package analysisflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"verbatrace/monolit/internal/models"
)

// growthPrompt is appended to the summary prompt only when the call keeps
// growth areas (spec 15.4); without it the step stays byte for byte as before.
const growthPrompt = `Дополнительно используй growth_context. growth_context.subject_speaker — ID участника, чья работа оценивается. open_growth_areas — ранее замеченные повторяющиеся недочёты этого участника; у каждого есть area_id, title и description. Для КАЖДОЙ переданной зоны верни ровно одну запись в growth_observations. verdict=repeated — в assessed_items этого разговора у участника есть недочёт того же смысла; укажи в item_ids карточки, где он виден. verdict=improved — в разговоре была ситуация, где этот недочёт мог проявиться, и участник справился; укажи в item_ids карточки, где это видно. verdict=not_applicable — такой ситуации в разговоре не было; item_ids пуст. Не ставь improved только потому, что недочёт нигде не упомянут. note — одна фраза с основанием, участника обозначай маркером {{speaker:ID}}. new_growth_areas — не больше трёх новых недочётов этого участника, которые влияют на оценку (gaps с affects_score=true), относятся к карточкам kind=question или kind=episode, а не kind=requirement, и не совпадают по смыслу ни с одной переданной зоной. title — до 60 символов, без имени и без маркера участника, в форме того, что нужно изменить: «Отвечает общими словами без примеров». description — одна-две фразы о том, что именно стоит делать иначе. item_ids — карточки-основания, не пусто. Единичную мелкую оговорку зоной не считай. Если недочётов нет, new_growth_areas=[].`

const maxNewGrowthAreas = 3

// growthProperties are the summary step's extra output fields. They exist only
// in this step's schema: neither the provider's root schema nor the result JSON
// carries them. Observations are asked for only when there are areas to judge.
func growthProperties(withObservations bool) map[string]any {
	props := map[string]any{
		"new_growth_areas": array(object(map[string]any{
			"title": str(), "description": str(), "item_ids": array(str()),
		})),
	}
	if withObservations {
		props["growth_observations"] = array(object(map[string]any{
			"area_id": str(), "verdict": enum(models.GrowthVerdictRepeated, models.GrowthVerdictImproved, models.GrowthVerdictNotApplicable),
			"item_ids": array(str()), "note": str(),
		}))
	}
	return props
}

func growthInput(growth *models.GrowthContext) map[string]any {
	areas := growth.OpenAreas
	if areas == nil {
		areas = []models.GrowthAreaRef{}
	}
	return map[string]any{"subject_speaker": growth.SubjectSpeaker, "open_growth_areas": areas}
}

// readGrowth takes the growth fields out of a summary answer.
func readGrowth(summary map[string]any) (models.GrowthOutcome, error) {
	var out models.GrowthOutcome
	raw, err := json.Marshal(map[string]any{"o": summary["growth_observations"], "n": summary["new_growth_areas"]})
	if err != nil {
		return out, err
	}
	var parsed struct {
		O []models.GrowthObservation `json:"o"`
		N []models.NewGrowthArea     `json:"n"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return out, errors.New("growth fields have an invalid shape")
	}
	out.Observations, out.NewAreas = parsed.O, parsed.N
	return out, nil
}

// checkGrowth is the soft validation of the growth fields: one observation per
// area passed in, no other areas, cited cards that exist, and new areas only on
// question or episode cards.
func checkGrowth(outcome models.GrowthOutcome, growth *models.GrowthContext, items []map[string]any) error {
	kinds := make(map[string]string, len(items))
	for _, item := range items {
		kinds[text(item["id"])] = text(item["kind"])
	}
	passed := make(map[string]bool, len(growth.OpenAreas))
	for _, area := range growth.OpenAreas {
		passed[area.ID] = true
	}
	seen := map[string]bool{}
	var problems []string
	for _, o := range outcome.Observations {
		switch {
		case !passed[o.AreaID]:
			problems = append(problems, fmt.Sprintf("growth_observations: area_id %q was not in open_growth_areas", o.AreaID))
		case seen[o.AreaID]:
			problems = append(problems, fmt.Sprintf("growth_observations: area_id %q has more than one observation", o.AreaID))
		}
		seen[o.AreaID] = true
		if (o.Verdict == models.GrowthVerdictRepeated || o.Verdict == models.GrowthVerdictImproved) && len(o.ItemIDs) == 0 {
			problems = append(problems, fmt.Sprintf("growth_observations: %s for %q must cite item_ids", o.Verdict, o.AreaID))
		}
		for _, id := range o.ItemIDs {
			if _, ok := kinds[id]; !ok {
				problems = append(problems, fmt.Sprintf("growth_observations: unknown item_id %q", id))
			}
		}
	}
	// In input order: the message goes into the retry input, whose hash keys the
	// task cache.
	for _, area := range growth.OpenAreas {
		if !seen[area.ID] {
			problems = append(problems, fmt.Sprintf("growth_observations: no observation for area_id %q", area.ID))
		}
	}
	if len(outcome.NewAreas) > maxNewGrowthAreas {
		problems = append(problems, fmt.Sprintf("new_growth_areas: at most %d", maxNewGrowthAreas))
	}
	for i, area := range outcome.NewAreas {
		if !nonempty(area.Title) || !nonempty(area.Description) || len(area.ItemIDs) == 0 {
			problems = append(problems, fmt.Sprintf("new_growth_areas[%d]: title, description and item_ids are required", i))
		}
		for _, id := range area.ItemIDs {
			kind, ok := kinds[id]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("new_growth_areas[%d]: unknown item_id %q", i, id))
			case kind == "requirement":
				problems = append(problems, fmt.Sprintf("new_growth_areas[%d]: item %q is a requirement, which the criteria already track", i, id))
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}
