package analysis

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/models"
)

type cachingInstructionRepository struct {
	analysisInstructionRepository
	version models.InstructionVersionText
	missing bool
	saved   map[uuid.UUID]string
}

func (r *cachingInstructionRepository) ReadableVersion(context.Context, models.AnalysisInstruction) (models.InstructionVersionText, error) {
	if r.missing {
		return models.InstructionVersionText{}, models.ErrAnalysisInstructionNotFound
	}
	return r.version, nil
}

func (r *cachingInstructionRepository) SaveVersionText(_ context.Context, id uuid.UUID, text string) error {
	if r.saved == nil {
		r.saved = map[uuid.UUID]string{}
	}
	r.saved[id] = text
	r.version.Text = &text
	return nil
}

type countingInstructionStorage struct {
	analysisInstructionStorage
	opens int
}

func (s *countingInstructionStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	s.opens++
	return s.analysisInstructionStorage.Open(ctx, path)
}

func TestReadInstructionContentCachesTextOfTheReadVersion(t *testing.T) {
	versionID := uuid.New()
	repo := &cachingInstructionRepository{version: models.InstructionVersionText{VersionID: versionID}}
	storage := &countingInstructionStorage{analysisInstructionStorage: analysisInstructionStorage{files: map[string]string{"i/rubric.md": "Выясни бюджет"}}}
	service := NewService(nil, nil, repo, nil, storage, nil, nil)
	instruction := models.AnalysisInstruction{ID: uuid.New(), Title: "Rubric", FilePath: "i/rubric.md", OriginalFilename: "rubric.md"}

	first, err := service.readInstructionContent(context.Background(), instruction)
	require.NoError(t, err)
	require.Equal(t, versionID, first.VersionID)
	require.Equal(t, "Выясни бюджет", strings.TrimSpace(first.Content))
	require.Equal(t, first.Content, repo.saved[versionID])

	second, err := service.readInstructionContent(context.Background(), instruction)
	require.NoError(t, err)
	require.Equal(t, first.Content, second.Content)
	require.Equal(t, versionID, second.VersionID)
	require.Equal(t, 1, storage.opens, "the second analysis must not extract the file again")
}

func TestReadInstructionContentWithoutVersionFallsBackToFile(t *testing.T) {
	repo := &cachingInstructionRepository{missing: true}
	storage := &countingInstructionStorage{analysisInstructionStorage: analysisInstructionStorage{files: map[string]string{"i/rubric.md": "Представься"}}}
	service := NewService(nil, nil, repo, nil, storage, nil, nil)

	content, err := service.readInstructionContent(context.Background(), models.AnalysisInstruction{ID: uuid.New(), FilePath: "i/rubric.md", OriginalFilename: "rubric.md"})
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, content.VersionID)
	require.Equal(t, "Представься", strings.TrimSpace(content.Content))
	require.Empty(t, repo.saved)
}
