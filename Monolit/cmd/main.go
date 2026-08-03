package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	adminAPI "verbatrace/monolit/internal/API/admin"
	analysisAPI "verbatrace/monolit/internal/API/analysis"
	analysisContextAPI "verbatrace/monolit/internal/API/analysis_context"
	instructionAPI "verbatrace/monolit/internal/API/analysis_instruction"
	analyticsAPI "verbatrace/monolit/internal/API/analytics"
	authAPI "verbatrace/monolit/internal/API/auth"
	billingAPI "verbatrace/monolit/internal/API/billing"
	"verbatrace/monolit/internal/API/call"
	callFolderAPI "verbatrace/monolit/internal/API/call_folder"
	companyAPI "verbatrace/monolit/internal/API/company"
	contactAPI "verbatrace/monolit/internal/API/contact"
	departmentAPI "verbatrace/monolit/internal/API/department"
	healthAPI "verbatrace/monolit/internal/API/health"
	invitationAPI "verbatrace/monolit/internal/API/invitation"
	monitoringAPI "verbatrace/monolit/internal/API/monitoring"
	notificationAPI "verbatrace/monolit/internal/API/notification"
	reportAPI "verbatrace/monolit/internal/API/report"
	searchAPI "verbatrace/monolit/internal/API/search"
	"verbatrace/monolit/internal/analyzer"
	"verbatrace/monolit/internal/config"
	"verbatrace/monolit/internal/httpserver"
	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/migrator"
	adminRepo "verbatrace/monolit/internal/repository/admin"
	analysisRepo "verbatrace/monolit/internal/repository/analysis"
	analysisContextRepo "verbatrace/monolit/internal/repository/analysis_context"
	analysisInstructionRepo "verbatrace/monolit/internal/repository/analysis_instruction"
	billingRepo "verbatrace/monolit/internal/repository/billing"
	callRepo "verbatrace/monolit/internal/repository/call"
	callFolderRepo "verbatrace/monolit/internal/repository/call_folder"
	companyRepo "verbatrace/monolit/internal/repository/company"
	contactRepo "verbatrace/monolit/internal/repository/contact"
	departmentRepo "verbatrace/monolit/internal/repository/department"
	invitationRepo "verbatrace/monolit/internal/repository/invitation"
	notificationRepo "verbatrace/monolit/internal/repository/notification"
	processingJobRepo "verbatrace/monolit/internal/repository/processing_job"
	refreshSessionRepo "verbatrace/monolit/internal/repository/refresh_session"
	reportRepo "verbatrace/monolit/internal/repository/report"
	searchRepo "verbatrace/monolit/internal/repository/search"
	transcriptionRepo "verbatrace/monolit/internal/repository/transcription"
	userRepo "verbatrace/monolit/internal/repository/user"
	userPreferencesRepo "verbatrace/monolit/internal/repository/user_preferences"
	adminService "verbatrace/monolit/internal/service/admin"
	analysisService "verbatrace/monolit/internal/service/analysis"
	analysisContextService "verbatrace/monolit/internal/service/analysis_context"
	analysisInstructionService "verbatrace/monolit/internal/service/analysis_instruction"
	analyticsService "verbatrace/monolit/internal/service/analytics"
	authService "verbatrace/monolit/internal/service/auth"
	billingService "verbatrace/monolit/internal/service/billing"
	callService "verbatrace/monolit/internal/service/call"
	callFolderService "verbatrace/monolit/internal/service/call_folder"
	companyService "verbatrace/monolit/internal/service/company"
	contactService "verbatrace/monolit/internal/service/contact"
	departmentService "verbatrace/monolit/internal/service/department"
	invitationService "verbatrace/monolit/internal/service/invitation"
	monitoringService "verbatrace/monolit/internal/service/monitoring"
	notificationService "verbatrace/monolit/internal/service/notification"
	processingService "verbatrace/monolit/internal/service/processing"
	reportService "verbatrace/monolit/internal/service/report"
	searchService "verbatrace/monolit/internal/service/search"
	transcriptionEditService "verbatrace/monolit/internal/service/transcriptionedit"
	"verbatrace/monolit/internal/storage/audio"
	avatarStorage "verbatrace/monolit/internal/storage/avatar"
	"verbatrace/monolit/internal/storage/instruction"
	reportStorage "verbatrace/monolit/internal/storage/report"
	"verbatrace/monolit/internal/transcriber"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

const (
	configPath      = "./.env"
	shutdownTimeout = 10 * time.Second

	audioUploadDirName       = "audio"
	avatarUploadDirName      = "avatars"
	instructionUploadDirName = "instructions"
	reportUploadDirName      = "reports"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startupLogger := logger.New("info", false)

	err := config.Load(configPath)
	if err != nil {
		startupLogger.Error(ctx, "failed to load config", zap.Error(err))
		return
	}

	appLogger := logger.New(config.AppConfig().Logger.Level(), config.AppConfig().Logger.AsJSON())
	//var cancel context.CancelFunc

	dbURI := config.AppConfig().Postgres.URI()
	if dbURI == "" {
		appLogger.Error(ctx, "postgres uri is empty")
		return
	}

	con, err := pgx.Connect(ctx, dbURI)
	if err != nil {
		appLogger.Error(ctx, "failed to connect to postgres", zap.Error(err))
		return
	}
	defer func() {
		if cerr := con.Close(context.Background()); cerr != nil {
			appLogger.Error(context.Background(), "failed to close postgres connection", zap.Error(cerr))
		}
	}()

	err = con.Ping(ctx)
	if err != nil {
		appLogger.Error(ctx, "failed to ping postgres", zap.Error(err))
	}

	sqlDB := stdlib.OpenDB(*con.Config().Copy())
	migrationsDIR := config.AppConfig().Postgres.MigrationDir()
	if migrationsDIR == "" {
		appLogger.Error(ctx, "migrations directory is empty")
		return
	}
	migratorRunner := migrator.NewMigrator(sqlDB, migrationsDIR)

	err = migratorRunner.Up()
	if err != nil {
		appLogger.Error(ctx, "failed to run migrator", zap.Error(err))
		return
	}
	uploadPath := config.AppConfig().Upload.Path()
	audioUploadPath := filepath.Join(uploadPath, audioUploadDirName)
	avatarUploadPath := filepath.Join(uploadPath, avatarUploadDirName)
	instructionUploadPath := filepath.Join(uploadPath, instructionUploadDirName)
	reportUploadPath := filepath.Join(uploadPath, reportUploadDirName)

	for _, dir := range []string{audioUploadPath, avatarUploadPath, instructionUploadPath, reportUploadPath} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			appLogger.Error(ctx, "failed to create upload directory", zap.String("path", dir), zap.Error(err))
			return
		}
	}

	healthHandler := healthAPI.NewHandler(
		healthAPI.DatabaseCheck(sqlDB),
		healthAPI.WritableDirectoryCheck("uploads.audio", audioUploadPath),
		healthAPI.WritableDirectoryCheck("uploads.avatars", avatarUploadPath),
		healthAPI.WritableDirectoryCheck("uploads.instructions", instructionUploadPath),
		healthAPI.WritableDirectoryCheck("uploads.reports", reportUploadPath),
		healthAPI.BinaryCheck("ffmpeg", config.AppConfig().Upload.FFmpegPath()),
		healthAPI.BinaryCheck("ffprobe", config.AppConfig().Upload.FFProbePath()),
	)

	audioStorage := audio.NewLocalStorage(audioUploadPath)
	avatarsStorage := avatarStorage.NewLocalStorage(avatarUploadPath)
	instructionStorage := instruction.NewLocalStorage(instructionUploadPath)
	reportsStorage := reportStorage.NewLocalStorage(reportUploadPath)

	adminRepository := adminRepo.NewRepository(sqlDB)
	analysisInstructionRepository := analysisInstructionRepo.NewRepository(sqlDB)
	analysisContextRepository := analysisContextRepo.NewRepository(sqlDB)
	analysisRepository := analysisRepo.NewRepository(sqlDB)
	callRepository := callRepo.NewRepository(sqlDB)
	callFolderRepository := callFolderRepo.NewRepository(sqlDB)
	contactRepository := contactRepo.NewRepository(sqlDB)
	userRepository := userRepo.NewUserRepository(sqlDB)
	userPreferencesRepository := userPreferencesRepo.NewRepository(sqlDB)
	refreshRepository := refreshSessionRepo.NewRepository(sqlDB)
	companyRepository := companyRepo.NewRepository(sqlDB)
	departmentRepository := departmentRepo.NewRepository(sqlDB)
	invitationRepository := invitationRepo.NewRepository(sqlDB)
	transcriptionRepository := transcriptionRepo.NewRepository(sqlDB)
	processingJobRepository := processingJobRepo.NewRepository(sqlDB)
	billingRepository := billingRepo.NewRepository(sqlDB)
	reportRepository := reportRepo.NewRepository(sqlDB)
	searchRepository := searchRepo.NewRepository(sqlDB)
	notificationRepository := notificationRepo.NewRepository(sqlDB)

	transcriberProvider, err := transcriber.NewFromConfig(config.AppConfig().Transcriber)
	if err != nil {
		appLogger.Error(ctx, "failed to configure transcriber", zap.Error(err))
		return
	}

	analyzerProvider, err := analyzer.NewFromConfig(config.AppConfig().Analyzer)
	if err != nil {
		appLogger.Error(ctx, "failed to configure analyzer", zap.Error(err))
		return
	}

	analysisSvc := analysisService.NewService(callRepository, transcriptionRepository, analysisInstructionRepository, analysisRepository, instructionStorage, analyzerProvider, appLogger)
	analysisSvc.SetProcessingJobRepository(processingJobRepository)
	analysisSvc.SetProcessingJobMaxAttempts(config.AppConfig().Worker.MaxAttempts())
	analysisSvc.SetPersonalizationReader(analysisContextRepository)
	analysisSvc.SetFolderInstructionReader(callFolderRepository)
	processingSvc := processingService.NewService(callRepository, transcriptionRepository, processingJobRepository, audioStorage, transcriberProvider, appLogger)
	processingSvc.SetProcessingJobMaxAttempts(config.AppConfig().Worker.MaxAttempts())
	processingSvc.SetAnalysisProcessor(analysisSvc)

	var workerDone <-chan struct{}
	if config.AppConfig().Worker.Enabled() {
		processingWorker := processingService.NewWorker(processingSvc, processingService.WorkerOptions{
			PollInterval: config.AppConfig().Worker.PollInterval(),
			Limit:        config.AppConfig().Worker.Limit(),
			RetryDelay:   config.AppConfig().Worker.RetryDelay(),
			StaleAfter:   config.AppConfig().Worker.StaleAfter(),
		}, appLogger)

		done := make(chan struct{})
		workerDone = done
		go func() {
			defer close(done)
			processingWorker.Run(ctx)
		}()
	} else {
		appLogger.Info(ctx, "processing worker disabled")
	}

	callSvc := callService.NewService(callRepository, companyRepository, departmentRepository, audioStorage, appLogger)
	callSvc.SetCallFolderRepository(callFolderRepository)
	callSvc.SetTranscriptionRepository(transcriptionRepository)
	callSvc.SetProcessingJobRepository(processingJobRepository)
	callSvc.SetProcessingJobMaxAttempts(config.AppConfig().Worker.MaxAttempts())
	callSvc.SetDurationDetector(audio.NewFFProbeDurationDetector(audioUploadPath, config.AppConfig().Upload.FFProbePath()))
	authSvc := authService.NewService(
		userRepository,
		refreshRepository,
		config.AppConfig().Auth.PasswordPepper(),
		config.AppConfig().Auth.JWTSecret(),
		config.AppConfig().Auth.AccessTokenTTL(),
		config.AppConfig().Auth.RefreshTokenSecret(),
		config.AppConfig().Auth.RefreshTokenTTL(),
		appLogger,
	)
	authSvc.SetBillingRepository(billingRepository)
	authSvc.SetSessionTrustAge(config.AppConfig().Auth.SessionTrustAge())
	authSvc.SetCompanyRepository(companyRepository)
	authSvc.SetPreferencesRepository(userPreferencesRepository)
	authSvc.SetAvatarStorage(avatarsStorage)
	adminSvc := adminService.NewService(adminRepository)
	adminSvc.SetCallReader(callRepository)
	adminSvc.SetAudioStorage(audioStorage)
	companySvc := companyService.NewService(companyRepository, appLogger)
	departmentSvc := departmentService.NewService(companyRepository, departmentRepository, appLogger)
	invitationSvc := invitationService.NewService(invitationRepository, userRepository, companyRepository, departmentRepository, appLogger)
	instructionSvc := analysisInstructionService.NewService(analysisInstructionRepository, companyRepository, departmentRepository, instructionStorage, appLogger)
	billingSvc := billingService.NewService(billingRepository)
	reportSvc := reportService.NewService(callRepository, analysisRepository, transcriptionRepository, reportRepository, reportsStorage)
	analyticsSvc := analyticsService.NewService(callRepository)
	analyticsSvc.SetCallFolderRepository(callFolderRepository)
	analyticsSvc.SetCompanyRepository(companyRepository)
	analyticsSvc.SetDepartmentRepository(departmentRepository)
	analyticsSvc.SetAnalyzer(analyzerProvider)
	analyticsSvc.SetReportRepository(reportRepository)
	analyticsSvc.SetReportStorage(reportsStorage)
	callFolderSvc := callFolderService.NewService(callFolderRepository, callRepository, companyRepository, departmentRepository)
	contactSvc := contactService.NewService(contactRepository, userRepository, callRepository)
	callFolderSvc.SetInstructionRepositories(analysisInstructionRepository, callFolderRepository)
	analysisContextSvc := analysisContextService.NewService(analysisContextRepository, companyRepository, departmentRepository)
	monitoringSvc := monitoringService.NewService(processingJobRepository, companyRepository)
	searchSvc := searchService.NewService(searchRepository)
	notificationSvc := notificationService.NewService(notificationRepository)
	billingSvc.SetCompanyRepository(companyRepository)
	callSvc.SetBillingLimiter(billingSvc)
	callSvc.SetTranscriptionModeResolver(billingSvc)
	companySvc.SetBillingLimiter(billingSvc)
	departmentSvc.SetBillingLimiter(billingSvc)
	invitationSvc.SetBillingLimiter(billingSvc)
	instructionSvc.SetBillingLimiter(billingSvc)
	reportSvc.SetBillingLimiter(billingSvc)
	invitationSvc.SetNotificationService(notificationSvc)

	adminHandler := adminAPI.NewHandler(adminSvc)
	callHandler := call.NewCallHandler(callSvc)
	callHandler.SetTranscriptionEditor(transcriptionEditService.NewService(sqlDB, callRepository, transcriptionRepository))
	callFolderHandler := callFolderAPI.NewHandler(callFolderSvc)
	contactHandler := contactAPI.NewHandler(contactSvc)
	authHandler := authAPI.NewAuthHandler(authSvc, config.AppConfig().Auth.AccessTokenTTL(), config.AppConfig().Auth.RefreshTokenTTL())
	companyHandler := companyAPI.NewCompanyHandler(companySvc)
	departmentHandler := departmentAPI.NewDepartmentHandler(departmentSvc)
	invitationHandler := invitationAPI.NewHandler(invitationSvc)
	instructionHandler := instructionAPI.NewHandler(instructionSvc)
	analysisContextHandler := analysisContextAPI.NewHandler(analysisContextSvc)
	analysisHandler := analysisAPI.NewHandler(analysisSvc)
	reportHandler := reportAPI.NewHandler(reportSvc)
	billingHandler := billingAPI.NewHandler(billingSvc)
	analyticsHandler := analyticsAPI.NewHandler(analyticsSvc)
	monitoringHandler := monitoringAPI.NewHandler(monitoringSvc)
	searchHandler := searchAPI.NewHandler(searchSvc)
	notificationHandler := notificationAPI.NewHandler(notificationSvc)

	r := httpserver.NewRouter(callHandler, callFolderHandler, contactHandler, authHandler, companyHandler, departmentHandler, instructionHandler, analysisContextHandler, analysisHandler, reportHandler, billingHandler, invitationHandler, analyticsHandler, monitoringHandler, searchHandler, notificationHandler, adminHandler, healthHandler, config.AppConfig().Auth.JWTSecret(), refreshRepository, appLogger)

	server := &http.Server{
		Addr:              config.AppConfig().HTTPConfig.Address(),
		Handler:           r,
		ReadHeaderTimeout: config.AppConfig().HTTPConfig.ReadTimeout(),
	}

	appLogger.Info(ctx, "api server started", zap.String("address", server.Addr))

	serverErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
			return
		}

		serverErr <- nil
	}()

	select {
	case <-ctx.Done():
		appLogger.Info(context.Background(), "shutdown signal received")
	case err := <-serverErr:
		if err != nil {
			appLogger.Error(context.Background(), "api server stopped with error", zap.Error(err))
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		appLogger.Error(context.Background(), "failed to shutdown api server", zap.Error(err))
	} else {
		appLogger.Info(context.Background(), "api server stopped")
	}

	if workerDone != nil {
		select {
		case <-workerDone:
			appLogger.Info(context.Background(), "processing worker shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), "processing worker shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
}
