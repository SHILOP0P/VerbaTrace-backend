package instruction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func TestLocalStorageLifecycle(t *testing.T) {
	storage := NewLocalStorage(t.TempDir())
	userID := uuid.New()
	instructionID := uuid.New()
	content := "Use this instruction"

	saved, err := storage.Save(context.Background(), models.SaveInstructionInput{
		InstructionUUID:  instructionID,
		Scope:            models.AnalysisInstructionScopePersonal,
		UserUUID:         uuid.NullUUID{UUID: userID, Valid: true},
		OriginalFilename: "guide.MD",
		Content:          strings.NewReader(content),
		MimeType:         "text/markdown",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	sum := sha256.Sum256([]byte(content))
	if saved.SizeBytes != int64(len(content)) || saved.ContentSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("saved file = %+v", saved)
	}

	file, err := storage.Open(context.Background(), saved.Path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, _ := io.ReadAll(file)
	_ = file.Close()
	if string(data) != content {
		t.Fatalf("content = %q", data)
	}
	windowsStylePath := strings.ReplaceAll(saved.Path, string(filepath.Separator), "\\")
	file, err = storage.Open(context.Background(), windowsStylePath)
	if err != nil {
		t.Fatalf("Open windows-style path: %v", err)
	}
	_ = file.Close()
	if err := storage.Delete(context.Background(), saved.Path); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := storage.Open(context.Background(), saved.Path); !errors.Is(err, models.ErrInstructionFileNotFound) {
		t.Fatalf("Open missing error = %v", err)
	}
}

func TestSaveScopesAndValidation(t *testing.T) {
	storage := NewLocalStorage(t.TempDir())
	companyID := uuid.New()
	departmentID := uuid.New()

	tests := []models.SaveInstructionInput{
		{
			InstructionUUID: uuid.New(), Scope: models.AnalysisInstructionScopeCompany,
			CompanyUUID:      uuid.NullUUID{UUID: companyID, Valid: true},
			OriginalFilename: "company.md", Content: strings.NewReader("company"),
		},
		{
			InstructionUUID: uuid.New(), Scope: models.AnalysisInstructionScopeDepartment,
			CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true}, DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
			OriginalFilename: "department.md", Content: strings.NewReader("department"),
		},
	}
	for _, input := range tests {
		if _, err := storage.Save(context.Background(), input); err != nil {
			t.Fatalf("Save scope %q: %v", input.Scope, err)
		}
	}

	invalid := []models.SaveInstructionInput{
		{},
		{Content: strings.NewReader("x"), OriginalFilename: "x.txt"},
		{Content: strings.NewReader("x"), OriginalFilename: "x.md", Scope: models.AnalysisInstructionScopePersonal},
		{Content: strings.NewReader("x"), OriginalFilename: "x.md", Scope: models.AnalysisInstructionScopeCompany},
		{Content: strings.NewReader("x"), OriginalFilename: "x.md", Scope: models.AnalysisInstructionScopeDepartment},
		{Content: strings.NewReader("x"), OriginalFilename: "x.md", Scope: "unknown"},
		{
			InstructionUUID: uuid.New(), Content: strings.NewReader(""), OriginalFilename: "empty.md",
			Scope: models.AnalysisInstructionScopePersonal, UserUUID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		},
	}
	for i, input := range invalid {
		if _, err := storage.Save(context.Background(), input); err == nil {
			t.Fatalf("invalid input %d unexpectedly succeeded", i)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	valid := models.SaveInstructionInput{
		InstructionUUID: uuid.New(), Content: strings.NewReader("x"), OriginalFilename: "x.md",
		Scope: models.AnalysisInstructionScopePersonal, UserUUID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	}
	if _, err := storage.Save(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save canceled error = %v", err)
	}
	if _, err := storage.Open(ctx, "x.md"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open canceled error = %v", err)
	}
	if err := storage.Delete(ctx, "x.md"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete canceled error = %v", err)
	}
	if _, err := safeLocalPath(storage.baseDir, filepath.Join("..", "escape.md")); !errors.Is(err, models.ErrInvalidInstructionPath) {
		t.Fatalf("unsafe path error = %v", err)
	}
}
