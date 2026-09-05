package privacy

import (
	"database/sql"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/storage"
	"verbatrace/monolit/internal/transcriber"
)

type Config struct {
	AudioBaseDir string
	FFmpegPath   string
	FFProbePath  string
}

type Service struct {
	db      *sql.DB
	audio   storage.AudioStorage
	config  Config
	log     logger.Logger
	now     func() time.Time
	cleaner transcriber.ArtifactCleaner
}

func (s *Service) SetArtifactCleaner(cleaner transcriber.ArtifactCleaner) { s.cleaner = cleaner }

func NewService(db *sql.DB, audio storage.AudioStorage, config Config, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}
	if config.FFmpegPath == "" {
		config.FFmpegPath = "ffmpeg"
	}
	if config.FFProbePath == "" {
		config.FFProbePath = "ffprobe"
	}
	return &Service{db: db, audio: audio, config: config, log: log, now: time.Now}
}
