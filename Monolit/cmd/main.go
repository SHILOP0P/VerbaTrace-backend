package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	actionAPI "verbatrace/monolit/internal/API/action"
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
	integrationAPI "verbatrace/monolit/internal/API/integration"
	invitationAPI "verbatrace/monolit/internal/API/invitation"
	monitoringAPI "verbatrace/monolit/internal/API/monitoring"
	notificationAPI "verbatrace/monolit/internal/API/notification"
	qualityReviewAPI "verbatrace/monolit/internal/API/quality_review"
	reportAPI "verbatrace/monolit/internal/API/report"
	scorecardAPI "verbatrace/monolit/internal/API/scorecard"
	searchAPI "verbatrace/monolit/internal/API/search"
	"verbatrace/monolit/internal/analyzer"
	analyzerMock "verbatrace/monolit/internal/analyzer/mock"
	"verbatrace/monolit/internal/assistant"
	"verbatrace/monolit/internal/config"
	"verbatrace/monolit/internal/httpserver"
	httpMiddleware "verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/integrationcrypto"
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
	integrationRepo "verbatrace/monolit/internal/repository/integration"
	invitationRepo "verbatrace/monolit/internal/repository/invitation"
	notificationRepo "verbatrace/monolit/internal/repository/notification"
	processingJobRepo "verbatrace/monolit/internal/repository/processing_job"
	refreshSessionRepo "verbatrace/monolit/internal/repository/refresh_session"
	reportRepo "verbatrace/monolit/internal/repository/report"
	searchRepo "verbatrace/monolit/internal/repository/search"
	transcriptionRepo "verbatrace/monolit/internal/repository/transcription"
	userRepo "verbatrace/monolit/internal/repository/user"
	userPreferencesRepo "verbatrace/monolit/internal/repository/user_preferences"
	actionService "verbatrace/monolit/internal/service/action"
	adminService "verbatrace/monolit/internal/service/admin"
	analysisService "verbatrace/monolit/internal/service/analysis"
	analysisContextService "verbatrace/monolit/internal/service/analysis_context"
	analysisInstructionService "verbatrace/monolit/internal/service/analysis_instruction"
	analyticsService "verbatrace/monolit/internal/service/analytics"
	analyticsFactsService "verbatrace/monolit/internal/service/analyticsfacts"
	authService "verbatrace/monolit/internal/service/auth"
	billingService "verbatrace/monolit/internal/service/billing"
	bitrix24Service "verbatrace/monolit/internal/service/bitrix24"
	callService "verbatrace/monolit/internal/service/call"
	callFolderService "verbatrace/monolit/internal/service/call_folder"
	callSubjectService "verbatrace/monolit/internal/service/callsubject"
	companyService "verbatrace/monolit/internal/service/company"
	contactService "verbatrace/monolit/internal/service/contact"
	deliveryService "verbatrace/monolit/internal/service/delivery"
	departmentService "verbatrace/monolit/internal/service/department"
	growthService "verbatrace/monolit/internal/service/growth"
	integrationService "verbatrace/monolit/internal/service/integration"
	invitationService "verbatrace/monolit/internal/service/invitation"
	monitoringService "verbatrace/monolit/internal/service/monitoring"
	notificationService "verbatrace/monolit/internal/service/notification"
	privacyService "verbatrace/monolit/internal/service/privacy"
	processingService "verbatrace/monolit/internal/service/processing"
	qualityReviewService "verbatrace/monolit/internal/service/qualityreview"
	reportService "verbatrace/monolit/internal/service/report"
	retentionService "verbatrace/monolit/internal/service/retention"
	scorecardService "verbatrace/monolit/internal/service/scorecard"
	searchService "verbatrace/monolit/internal/service/search"
	supportAccessService "verbatrace/monolit/internal/service/supportaccess"
	teamAnalyticsService "verbatrace/monolit/internal/service/teamanalytics"
	transcriptionEditService "verbatrace/monolit/internal/service/transcriptionedit"
	"verbatrace/monolit/internal/storage/audio"
	avatarStorage "verbatrace/monolit/internal/storage/avatar"
	"verbatrace/monolit/internal/storage/instruction"
	reportStorage "verbatrace/monolit/internal/storage/report"
	"verbatrace/monolit/internal/transcriber"
	transcriberMock "verbatrace/monolit/internal/transcriber/mock"

	"github.com/google/uuid"
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
	privacySvc := privacyService.NewService(sqlDB, audioStorage, privacyService.Config{AudioBaseDir: audioUploadPath, FFmpegPath: config.AppConfig().Upload.FFmpegPath(), FFProbePath: config.AppConfig().Upload.FFProbePath()}, appLogger)

	transcriberProvider, err := transcriber.NewFromConfig(config.AppConfig().Transcriber)
	if err != nil {
		appLogger.Error(ctx, "failed to configure transcriber", zap.Error(err))
		return
	}
	if cleaner, ok := transcriberProvider.(transcriber.ArtifactCleaner); ok {
		privacySvc.SetArtifactCleaner(cleaner)
	}

	analyzerProvider, err := analyzer.NewFromConfig(config.AppConfig().Analyzer)
	if err != nil {
		appLogger.Error(ctx, "failed to configure analyzer", zap.Error(err))
		return
	}

	analysisSvc := analysisService.NewService(callRepository, transcriptionRepository, analysisInstructionRepository, analysisRepository, instructionStorage, analyzerProvider, appLogger)
	analysisSvc.SetSandboxAnalyzer(analyzerMock.New("sandbox-deterministic-v1"))
	analysisSvc.SetProcessingJobRepository(processingJobRepository)
	analysisSvc.SetProcessingJobMaxAttempts(config.AppConfig().Worker.MaxAttempts())
	analysisSvc.SetPersonalizationReader(analysisContextRepository)
	analysisSvc.SetFolderInstructionReader(callFolderRepository)
	analysisSvc.SetPrivacyContextReader(privacySvc)
	analysisSvc.SetMembershipRepositories(companyRepository, departmentRepository)
	processingSvc := processingService.NewService(callRepository, transcriptionRepository, processingJobRepository, audioStorage, transcriberProvider, appLogger)
	processingSvc.SetSandboxTranscriber(transcriberMock.New())
	processingSvc.SetProcessingJobMaxAttempts(config.AppConfig().Worker.MaxAttempts())
	processingSvc.SetAnalysisProcessor(analysisSvc)
	processingSvc.SetPrivacyManager(privacySvc)

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
	callSvc.SetPrivacyAdmissionResolver(privacySvc)
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
	adminSvc.SetMediaPrivacyGuard(privacySvc)
	companySvc := companyService.NewService(companyRepository, appLogger)
	departmentSvc := departmentService.NewService(companyRepository, departmentRepository, appLogger)
	invitationSvc := invitationService.NewService(invitationRepository, userRepository, companyRepository, departmentRepository, appLogger)
	instructionSvc := analysisInstructionService.NewService(analysisInstructionRepository, companyRepository, departmentRepository, instructionStorage, appLogger)
	billingSvc := billingService.NewService(billingRepository)
	// A billing repository that cannot serve one of the credit interfaces used
	// to panic on the first request that needed it. Fail at startup instead.
	if err := billingSvc.SetCreditRepository(billingRepository); err != nil {
		appLogger.Error(ctx, "failed to wire billing credit repository", zap.Error(err))
		return
	}
	creditReconciliationDone := billingService.NewReconciliationWorker(billingRepository, appLogger).Run(ctx)
	processingSvc.SetCreditMeter(billingSvc)
	analysisSvc.SetCreditMeter(billingSvc)
	reportSvc := reportService.NewService(callRepository, analysisRepository, transcriptionRepository, reportRepository, reportsStorage)
	analyticsSvc := analyticsService.NewService(callRepository)
	analyticsSvc.SetCallFolderRepository(callFolderRepository)
	analyticsSvc.SetTeamAnalyticsGate(billingSvc)
	callFolderSvc := callFolderService.NewService(callFolderRepository, callRepository, companyRepository, departmentRepository)
	contactSvc := contactService.NewService(contactRepository, userRepository, callRepository)
	callFolderSvc.SetInstructionRepositories(analysisInstructionRepository, callFolderRepository)
	analysisContextSvc := analysisContextService.NewService(analysisContextRepository, companyRepository, departmentRepository)
	monitoringSvc := monitoringService.NewService(processingJobRepository, companyRepository)
	searchSvc := searchService.NewService(searchRepository)
	notificationSvc := notificationService.NewService(notificationRepository)
	billingSvc.SetCompanyRepository(companyRepository)
	billingSvc.SetDepartmentRepository(departmentRepository)
	callSvc.SetBillingLimiter(billingSvc)
	callSvc.SetCreditReleaser(billingRepository)
	callSvc.SetTranscriptionModeResolver(billingSvc)
	companySvc.SetBillingLimiter(billingSvc)
	departmentSvc.SetBillingLimiter(billingSvc)
	invitationSvc.SetBillingLimiter(billingSvc)
	instructionSvc.SetBillingLimiter(billingSvc)
	instructionSvc.SetCompanyStateReader(sqlDB)
	reportSvc.SetBillingLimiter(billingSvc)
	invitationSvc.SetNotificationService(notificationSvc)
	invitationSvc.SetPreferencesReader(userPreferencesRepository)
	companySvc.SetNotificationService(notificationSvc)
	analysisSvc.SetNotificationService(notificationSvc)

	adminHandler := adminAPI.NewHandler(adminSvc)
	// Undoing a company deletion is the superadmin's own section of the panel,
	// and the only place the operation is reachable from.
	adminHandler.SetCompanyLifecycleService(companySvc)
	// Whom a call counts for is decided again whenever its speakers can have
	// changed: after transcription, on role and transcript edits, before analysis.
	callSubjectSvc := callSubjectService.NewService(sqlDB, appLogger)
	callSubjectSvc.SetNotificationService(notificationSvc)
	// Analytics reads facts projected from each call's effective analysis, never
	// result_json. Every change that moves a number re-projects the call.
	factsSvc := analyticsFactsService.NewService(sqlDB, appLogger)
	// Growth areas ride on the summary step of the analysis; a call that changes
	// hands or becomes shared loses the observations it gave.
	growthSvc := growthService.NewService(sqlDB, appLogger)
	callSubjectSvc.SetChangeHook(func(ctx context.Context, callID uuid.UUID) {
		factsSvc.Refresh(ctx, callID)
		growthSvc.Reconcile(ctx, callID)
	})
	analysisSvc.SetFactsProjector(factsSvc)
	analysisSvc.SetGrowth(growthSvc)
	// Digests and alerts go to the bell for real; mail and Telegram only reach
	// the queue, whose one sender for now logs the message.
	teamAnalyticsSvc := teamAnalyticsService.NewService(sqlDB, appLogger)
	deliverySvc := deliveryService.NewService(sqlDB, appLogger, config.AppConfig().Notify.PublicAppURL())
	notificationSender, err := deliveryService.NewSender(config.AppConfig().Notify.Sender(), appLogger)
	if err != nil {
		appLogger.Error(ctx, "invalid notification sender", zap.Error(err))
		return
	}
	processingSvc.SetSubjectRefresher(callSubjectSvc)
	transcriptionEditor := transcriptionEditService.NewService(sqlDB, callRepository, transcriptionRepository)
	transcriptionEditor.SetSubjectResolver(callSubjectSvc)
	callHandler := call.NewCallHandler(callSvc)
	callHandler.SetTranscriptionEditor(transcriptionEditor)
	callHandler.SetPrivacyService(privacySvc)
	callHandler.SetCallAccessReader(callRepository)
	callHandler.SetCallSubjectsService(callSubjectSvc)
	callFolderHandler := callFolderAPI.NewHandler(callFolderSvc)
	contactHandler := contactAPI.NewHandler(contactSvc)
	authHandler := authAPI.NewAuthHandler(authSvc, config.AppConfig().Auth.AccessTokenTTL(), config.AppConfig().Auth.RefreshTokenTTL())
	companyHandler := companyAPI.NewCompanyHandler(companySvc)
	departmentHandler := departmentAPI.NewDepartmentHandler(departmentSvc)
	invitationHandler := invitationAPI.NewHandler(invitationSvc)
	instructionHandler := instructionAPI.NewHandler(instructionSvc)
	scorecardSvc := scorecardService.NewService(sqlDB, instructionSvc, analyzerProvider, instructionStorage, appLogger)
	scorecardSvc.SetCreditMeter(billingSvc)
	scorecardSvc.SetNotificationService(notificationSvc)
	analysisSvc.SetScorecardPlanner(scorecardSvc)
	scorecardHandler := scorecardAPI.NewHandler(scorecardSvc)
	// Compiling spends credits, so it runs only where calls are processed.
	var scorecardWorkerDone, factsWorkerDone, outboxWorkerDone, digestWorkerDone <-chan struct{}
	scorecardSvc.SetAliasHook(analyticsFactsService.Rekey)
	if config.AppConfig().Worker.Enabled() {
		scorecardWorkerDone = scorecardService.NewWorker(scorecardSvc, 0).Run(ctx)
		factsWorkerDone = analyticsFactsService.NewWorker(factsSvc, callSubjectSvc, 0, 0).Run(ctx)
		outboxWorkerDone = deliveryService.NewWorker(sqlDB, notificationSender, appLogger, 0, 0).Run(ctx)
		digestWorkerDone = deliveryService.NewDigests(deliverySvc, teamAnalyticsSvc).Run(ctx, 0)
	}
	analysisContextHandler := analysisContextAPI.NewHandler(analysisContextSvc)
	analysisHandler := analysisAPI.NewHandler(analysisSvc)
	qualityReviewSvc := qualityReviewService.NewService(sqlDB)
	qualityReviewHandler := qualityReviewAPI.NewHandler(qualityReviewSvc)
	actionSvc := actionService.NewService(sqlDB)
	actionHandler := actionAPI.NewHandler(actionSvc)
	actionWorkerDone := actionService.NewWorker(actionSvc, time.Hour, 500).Run(ctx)
	actionOverdueWorkerDone := actionService.NewOverdueWorker(actionSvc, time.Hour, 500).Run(ctx)
	// Membership housekeeping: expired invitations, transfer requests and
	// ownership offers must stop looking actionable on their own.
	invitationExpiryWorkerDone := invitationService.NewExpiryWorker(invitationSvc, time.Hour).Run(ctx)
	departmentTransferWorkerDone := departmentService.NewTransferExpiryWorker(departmentSvc, time.Hour).Run(ctx)
	membershipMaintenanceWorkerDone := companyService.NewMembershipMaintenanceWorker(companySvc, time.Hour).Run(ctx)
	retentionSvc := retentionService.NewService(sqlDB, audioStorage, reportsStorage, instructionStorage, appLogger)
	callRetentionWorkerDone := retentionService.NewCallWorker(retentionSvc, config.AppConfig().Worker.CallRetentionInterval(), config.AppConfig().Worker.CallRetentionBatch()).Run(ctx)
	instructionRetentionWorkerDone := retentionService.NewInstructionWorker(retentionSvc, config.AppConfig().Worker.InstructionRetentionInterval(), config.AppConfig().Worker.InstructionRetentionBatch()).Run(ctx)
	reportHandler := reportAPI.NewHandler(reportSvc)
	billingHandler := billingAPI.NewHandler(billingSvc)
	analyticsHandler := analyticsAPI.NewHandler(analyticsSvc)
	analyticsHandler.SetTeamAnalytics(teamAnalyticsSvc)
	analyticsHandler.SetGrowth(growthSvc)
	monitoringHandler := monitoringAPI.NewHandler(monitoringSvc)
	searchHandler := searchAPI.NewHandler(searchSvc)
	embeddingKey := firstConfigured(os.Getenv("EMBEDDING_API_KEY"), os.Getenv("ANALYZER_API_KEY"))
	assistantKey := firstConfigured(os.Getenv("ASSISTANT_API_KEY"), os.Getenv("ANALYZER_API_KEY"))
	embeddingModel := firstConfigured(os.Getenv("EMBEDDING_MODEL"), "openai/text-embedding-3-small")
	assistantModel := firstConfigured(os.Getenv("ASSISTANT_MODEL"), os.Getenv("ANALYZER_MODEL"))
	assistantProvider := assistant.NewOpenRouterProvider(embeddingKey, assistantKey, embeddingModel, assistantModel, 1536)
	assistantSvc := assistant.NewService(sqlDB, assistantProvider, assistant.GeneratorFor(assistantProvider))
	assistantSvc.SetCreditMeter(billingSvc)
	searchHandler.SetAssistant(assistantSvc)
	assistantRecoveryWorkerDone := assistantSvc.RunRecoveryWorker(ctx)
	var assistantIndexWorkerDone <-chan struct{}
	if config.AppConfig().Worker.Enabled() {
		assistantIndexWorkerDone = assistantSvc.RunIndexWorker(ctx)
	}
	notificationHandler := notificationAPI.NewHandler(notificationSvc)
	notificationHandler.SetSubscriptions(deliverySvc)
	var integrationCipher *integrationcrypto.Cipher
	if rawKey := os.Getenv("INTEGRATION_MASTER_KEY_BASE64"); rawKey != "" {
		integrationCipher, err = integrationcrypto.NewFromBase64(rawKey, 1)
		if err != nil {
			appLogger.Error(ctx, "invalid integration encryption key", zap.Error(err))
			return
		}
	} else {
		appLogger.Warn(ctx, "integration ingest disabled until INTEGRATION_MASTER_KEY_BASE64 is configured")
	}
	integrationRepository := integrationRepo.NewRepository(sqlDB, integrationCipher)
	integrationStagingDir := filepath.Join("uploads", "integration-staging")
	integrationSvc := integrationService.NewService(integrationRepository, billingSvc, integrationCipher, integrationStagingDir)
	integrationHandler := integrationAPI.NewHandler(integrationSvc)
	supportAccessSvc := supportAccessService.NewService(sqlDB)
	integrationHandler.SetSupportAccessService(supportAccessSvc)
	adminHandler.SetSupportAccessAuthorizer(supportAccessSvc)
	actionHandler.SetSupportAccessAuthorizer(supportAccessSvc)
	var supportAccessWorkerDone <-chan struct{}
	if config.AppConfig().Worker.Enabled() {
		supportAccessWorkerDone = supportAccessSvc.RunExpiryWorker(ctx)
	}
	bitrixSvc := bitrix24Service.NewService(sqlDB, integrationCipher, bitrix24Service.Config{
		ClientID: os.Getenv("BITRIX24_CLIENT_ID"), ClientSecret: os.Getenv("BITRIX24_CLIENT_SECRET"),
		RedirectURI: os.Getenv("BITRIX24_REDIRECT_URI"), TokenURL: os.Getenv("BITRIX24_TOKEN_URL"), PublicBaseURL: os.Getenv("PUBLIC_APP_URL"), EventToken: os.Getenv("BITRIX24_APPLICATION_TOKEN"),
	})
	integrationHandler.SetBitrix24Service(bitrixSvc)
	bitrixSvc.SetAppURL(config.AppConfig().Notify.PublicAppURL())
	// After an analysis: the alert about a failed call, and the summary in the
	// CRM card of a call that came from Bitrix24. A published QA revision updates
	// that summary too.
	analysisSvc.SetAlerts(callHooks{deliverySvc.CallAnalyzed, bitrixSvc.QueueCRMNote})
	qualityReviewSvc.SetFactsProjector(callHooks{factsSvc.Refresh, bitrixSvc.QueueCRMNote})
	// A frozen company stops importing calls, and the portal only learns why if
	// we tell it: its own event hook is one-way.
	companySvc.SetFreezeNotifier(bitrixSvc)
	var bitrixWorkerDone <-chan struct{}
	var bitrixReconcilerDone <-chan struct{}
	var bitrixBackfillDone <-chan struct{}
	if config.AppConfig().Worker.Enabled() && integrationCipher != nil {
		bitrixWorkerDone = bitrixSvc.RunActionSyncWorker(ctx)
		bitrixReconcilerDone = bitrixSvc.RunReconciler(ctx, integrationSvc)
		bitrixBackfillDone = bitrixSvc.RunBackfillWorker(ctx, integrationSvc)
	}
	var integrationWorkerDone <-chan struct{}
	var webhookWorkerDone <-chan struct{}
	if config.AppConfig().Worker.Enabled() && integrationCipher != nil {
		integrationWorkerDone = integrationService.NewWorker(integrationRepository, callSvc, integrationCipher, appLogger, integrationStagingDir).Run(ctx)
		webhookWorkerDone = integrationService.NewWebhookWorker(integrationRepository, appLogger).Run(ctx)
	}
	var privacyMediaWorkerDone <-chan struct{}
	var privacyCleanupWorkerDone <-chan struct{}
	if config.AppConfig().Worker.Enabled() {
		privacyMediaWorkerDone = privacySvc.RunMediaWorker(ctx)
		privacyCleanupWorkerDone = privacySvc.RunProviderCleanupWorker(ctx)
	}

	r := httpserver.NewRouter(callHandler, callFolderHandler, contactHandler, authHandler, companyHandler, departmentHandler, instructionHandler, scorecardHandler, analysisContextHandler, analysisHandler, qualityReviewHandler, actionHandler, reportHandler, billingHandler, invitationHandler, analyticsHandler, monitoringHandler, searchHandler, notificationHandler, adminHandler, integrationHandler, healthHandler, config.AppConfig().Auth.JWTSecret(), refreshRepository, httpMiddleware.CompanyFreeze(sqlDB), appLogger)

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
	if privacyMediaWorkerDone != nil {
		select {
		case <-privacyMediaWorkerDone:
			appLogger.Info(context.Background(), "privacy media worker shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), "privacy media worker shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
	for name, workerDone := range map[string]<-chan struct{}{
		"invitation expiry worker":      invitationExpiryWorkerDone,
		"department transfer worker":    departmentTransferWorkerDone,
		"membership maintenance worker": membershipMaintenanceWorkerDone,
		"scorecard worker":              scorecardWorkerDone,
		"analytics facts worker":        factsWorkerDone,
		"outbound message worker":       outboxWorkerDone,
		"weekly digest worker":          digestWorkerDone,
	} {
		if workerDone == nil {
			continue
		}
		select {
		case <-workerDone:
			appLogger.Info(context.Background(), name+" shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), name+" shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
	if privacyCleanupWorkerDone != nil {
		select {
		case <-privacyCleanupWorkerDone:
			appLogger.Info(context.Background(), "privacy provider cleanup worker shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), "privacy provider cleanup worker shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
	if integrationWorkerDone != nil {
		select {
		case <-integrationWorkerDone:
			appLogger.Info(context.Background(), "integration ingest worker shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), "integration ingest worker shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
	if webhookWorkerDone != nil {
		select {
		case <-webhookWorkerDone:
			appLogger.Info(context.Background(), "integration webhook worker shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), "integration webhook worker shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
	if bitrixWorkerDone != nil {
		select {
		case <-bitrixWorkerDone:
			appLogger.Info(context.Background(), "Bitrix24 task sync worker shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), "Bitrix24 task sync worker shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
	if bitrixReconcilerDone != nil {
		select {
		case <-bitrixReconcilerDone:
			appLogger.Info(context.Background(), "Bitrix24 reconciliation worker shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), "Bitrix24 reconciliation worker shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
	if bitrixBackfillDone != nil {
		select {
		case <-bitrixBackfillDone:
			appLogger.Info(context.Background(), "Bitrix24 backfill worker shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), "Bitrix24 backfill worker shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
	if supportAccessWorkerDone != nil {
		select {
		case <-supportAccessWorkerDone:
			appLogger.Info(context.Background(), "support access expiry worker shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), "support access expiry worker shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
	if assistantIndexWorkerDone != nil {
		select {
		case <-assistantIndexWorkerDone:
			appLogger.Info(context.Background(), "assistant index worker shutdown completed")
		case <-shutdownCtx.Done():
			appLogger.Warn(context.Background(), "assistant index worker shutdown timed out", zap.Error(shutdownCtx.Err()))
		}
	}
	select {
	case <-assistantRecoveryWorkerDone:
		appLogger.Info(context.Background(), "assistant recovery worker shutdown completed")
	case <-shutdownCtx.Done():
		appLogger.Warn(context.Background(), "assistant recovery worker shutdown timed out", zap.Error(shutdownCtx.Err()))
	}
	select {
	case <-creditReconciliationDone:
		appLogger.Info(context.Background(), "credit reconciliation worker shutdown completed")
	case <-shutdownCtx.Done():
		appLogger.Warn(context.Background(), "credit reconciliation worker shutdown timed out", zap.Error(shutdownCtx.Err()))
	}
	select {
	case <-actionWorkerDone:
		appLogger.Info(context.Background(), "action worker shutdown completed")
	case <-shutdownCtx.Done():
		appLogger.Warn(context.Background(), "action worker shutdown timed out", zap.Error(shutdownCtx.Err()))
	}
	select {
	case <-actionOverdueWorkerDone:
		appLogger.Info(context.Background(), "action overdue worker shutdown completed")
	case <-shutdownCtx.Done():
		appLogger.Warn(context.Background(), "action overdue worker shutdown timed out", zap.Error(shutdownCtx.Err()))
	}
	select {
	case <-callRetentionWorkerDone:
		appLogger.Info(context.Background(), "call retention worker shutdown completed")
	case <-shutdownCtx.Done():
		appLogger.Warn(context.Background(), "call retention worker shutdown timed out", zap.Error(shutdownCtx.Err()))
	}
	select {
	case <-instructionRetentionWorkerDone:
		appLogger.Info(context.Background(), "instruction retention worker shutdown completed")
	case <-shutdownCtx.Done():
		appLogger.Warn(context.Background(), "instruction retention worker shutdown timed out", zap.Error(shutdownCtx.Err()))
	}
}

func firstConfigured(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// callHooks runs several reactions to one event about a call, in order.
type callHooks []func(context.Context, uuid.UUID)

func (hooks callHooks) CallAnalyzed(ctx context.Context, callID uuid.UUID) { hooks.run(ctx, callID) }
func (hooks callHooks) Refresh(ctx context.Context, callID uuid.UUID)      { hooks.run(ctx, callID) }

func (hooks callHooks) run(ctx context.Context, callID uuid.UUID) {
	for _, hook := range hooks {
		hook(ctx, callID)
	}
}
