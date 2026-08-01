package instruction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"verbatrace/monolit/internal/models"
)

func (l *LocalStorage) Save(ctx context.Context, input models.SaveInstructionInput) (models.SavedInstructionFile, error) {
	if input.Content == nil {
		return models.SavedInstructionFile{}, models.ErrInvalidAnalysisInstructionInput
	}

	ext := strings.ToLower(filepath.Ext(input.OriginalFilename))
	if ext != ".md" {
		return models.SavedInstructionFile{}, models.ErrUnsupportedInstructionType
	}

	relativeDir, err := instructionRelativeDir(input)
	if err != nil {
		return models.SavedInstructionFile{}, err
	}

	if err := os.MkdirAll(filepath.Join(l.baseDir, relativeDir), 0755); err != nil {
		return models.SavedInstructionFile{}, fmt.Errorf("creating instruction directory failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return models.SavedInstructionFile{}, ctx.Err()
	default:
	}

	relativePath := filepath.Join(relativeDir, input.InstructionUUID.String()+ext)
	fullPath := filepath.Join(l.baseDir, relativePath)

	dst, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return models.SavedInstructionFile{}, fmt.Errorf("create instruction file failed: %w", err)
	}
	defer func() { _ = dst.Close() }()

	hash := sha256.New()
	sizeBytes, err := io.Copy(io.MultiWriter(dst, hash), input.Content)
	if err != nil {
		return models.SavedInstructionFile{}, fmt.Errorf("save instruction file failed: %w", err)
	}
	if sizeBytes == 0 {
		_ = os.Remove(fullPath)
		return models.SavedInstructionFile{}, models.ErrInvalidAnalysisInstructionInput
	}

	return models.SavedInstructionFile{
		Path:          relativePath,
		MimeType:      input.MimeType,
		SizeBytes:     sizeBytes,
		ContentSHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func (l *LocalStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	fullPath, err := safeLocalPath(l.baseDir, path)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %w", models.ErrInstructionFileNotFound, err)
		}

		return nil, fmt.Errorf("open instruction file failed: %w", err)
	}

	return file, nil
}

func (l *LocalStorage) Delete(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fullPath, err := safeLocalPath(l.baseDir, path)
	if err != nil {
		return err
	}

	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete instruction file failed: %w", err)
	}
	return nil
}

func instructionRelativeDir(input models.SaveInstructionInput) (string, error) {
	switch input.Scope {
	case models.AnalysisInstructionScopePersonal:
		if !input.UserUUID.Valid {
			return "", models.ErrInvalidAnalysisInstructionInput
		}
		return filepath.Join("personal", input.UserUUID.UUID.String()), nil
	case models.AnalysisInstructionScopeCompany:
		if !input.CompanyUUID.Valid {
			return "", models.ErrInvalidAnalysisInstructionInput
		}
		return filepath.Join("companies", input.CompanyUUID.UUID.String(), "company"), nil
	case models.AnalysisInstructionScopeDepartment:
		if !input.CompanyUUID.Valid || !input.DepartmentUUID.Valid {
			return "", models.ErrInvalidAnalysisInstructionInput
		}
		return filepath.Join("companies", input.CompanyUUID.UUID.String(), "departments", input.DepartmentUUID.UUID.String()), nil
	default:
		return "", models.ErrInvalidAnalysisInstructionInput
	}
}

func safeLocalPath(baseDir string, path string) (string, error) {
	normalizedPath := filepath.FromSlash(strings.ReplaceAll(path, "\\", "/"))
	cleanPath := filepath.Clean(normalizedPath)

	if cleanPath == "." || cleanPath == ".." || filepath.IsAbs(cleanPath) {
		return "", models.ErrInvalidInstructionPath
	}

	if strings.HasPrefix(cleanPath, ".."+string(os.PathSeparator)) {
		return "", models.ErrInvalidInstructionPath
	}

	return filepath.Join(baseDir, cleanPath), nil
}
