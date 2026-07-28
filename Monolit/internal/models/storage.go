package models

import (
	"io"

	"github.com/google/uuid"
)

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

type SaveInput struct {
	CallID           uuid.UUID
	OriginalFilename string
	Content          io.Reader
	SizeBytes        int64
	MimeType         string
}

type SavedFile struct {
	Path             string
	OriginalFilename string
	MimeType         string
	SizeBytes        int64
}

type SpeakerCandidate struct {
	Label       string
	Description string
	Kind        string
}

type File struct {
	Content           io.ReadCloser
	ReadSeeker        io.ReadSeeker
	Path              string
	OriginalFilename  string
	MimeType          string
	SizeBytes         int64
	SpeakerCandidates []SpeakerCandidate
}
