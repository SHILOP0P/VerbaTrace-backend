package models

type TranscriptionResult struct {
	Text     string
	Segments []TranscriptionSegment
	Words    []TranscriptionWord
	Language *string
}
