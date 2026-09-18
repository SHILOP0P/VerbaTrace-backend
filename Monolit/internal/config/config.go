package config

import (
	"errors"
	"os"

	"verbatrace/monolit/internal/config/env"

	"github.com/joho/godotenv"
)

var appConfig *config

type config struct {
	HTTPConfig  HTTPConfig
	Postgres    PostgresConfig
	Upload      UploadConfig
	Logger      LoggerConfig
	Auth        AuthConfig
	Worker      WorkerConfig
	Transcriber TranscriberConfig
	Analyzer    AnalyzerConfig
	Notify      NotifyConfig
}

func NewConfig() *config {
	return &config{}
}

func Load(path ...string) error {
	err := godotenv.Load(path...)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	httpCfg, err := env.NewHTTPConfig()
	if err != nil {
		return err
	}

	postgresCfg, err := env.NewPostgresConfig()
	if err != nil {
		return err
	}

	uploadCfg, err := env.NewUploadConfig()
	if err != nil {
		return err
	}

	loggerCfg, err := env.NewLoggerConfig()
	if err != nil {
		return err
	}

	authCfg, err := env.NewAuthConfig()
	if err != nil {
		return err
	}

	workerCfg, err := env.NewWorkerConfig()
	if err != nil {
		return err
	}

	transcriberCfg, err := env.NewTranscriberConfig()
	if err != nil {
		return err
	}

	analyzerCfg, err := env.NewAnalyzerConfig()
	if err != nil {
		return err
	}

	notifyCfg, err := env.NewNotifyConfig()
	if err != nil {
		return err
	}

	appConfig = &config{
		HTTPConfig:  httpCfg,
		Postgres:    postgresCfg,
		Upload:      uploadCfg,
		Logger:      loggerCfg,
		Auth:        authCfg,
		Worker:      workerCfg,
		Transcriber: transcriberCfg,
		Analyzer:    analyzerCfg,
		Notify:      notifyCfg,
	}
	return nil
}

func AppConfig() *config {
	return appConfig
}
