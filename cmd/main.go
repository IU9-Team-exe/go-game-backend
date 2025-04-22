package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "team_exe/docs"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	httpSwagger "github.com/swaggo/http-swagger"

	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	authDelivery "team_exe/internal/delivery/auth"
	gameDelivery "team_exe/internal/delivery/game"
	//katagoDelivery "team_exe/internal/delivery/katago"
	taskDelivery "team_exe/internal/delivery/tasks"
	ownMiddleware "team_exe/internal/middleware"
	internalRepository "team_exe/internal/repository"
	katagoUseCase "team_exe/internal/usecase/katago"
	//katagoProto "team_exe/microservices/proto"
)

type mainDeliveryHandler struct {
	auth *authDelivery.AuthHandler
	game *gameDelivery.GameHandler
	task *taskDelivery.TaskHandler
}

type dataBaseAdapters struct {
	redisAdapter        *adapters.AdapterRedis
	mongoAdapter        *adapters.AdapterMongo
	llmAdapter          *adapters.LlmAdapter
	resultServerAdapter *adapters.ResultServerAdapter
}

// @version 1.0
// @description Документация API авторизации и пользователей
// @host localhost:8080
// @BasePath /api

// @securityDefinitions.apikey ApiKeyAuth
// @in cookie
// @name sessionID

func main() {
	logger := NewLogger()
	cfg, err := bootstrap.Setup(".env")
	if err != nil {
		logger.Error("Failed to setup configuration", zap.Error(err))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleShutdown(cancel, logger)

	databaseAdapters := initDatabaseAdapters(ctx, logger, *cfg)
	defer databaseAdapters.mongoAdapter.Close(ctx)
	defer databaseAdapters.redisAdapter.Close(ctx)

	r := chi.NewRouter()
	handlers := initializeDeliveryHandlers(*cfg, logger, databaseAdapters)
	handlers.Router(r, cfg.IsLocalCors)

	port := ":8080"
	logger.Infof("Server is running on port %s", port)
	if err := http.ListenAndServe(port, r); err != nil {
		logger.Fatal("Failed to start server", zap.Error(err))
	}
}

func NewLogger() *zap.SugaredLogger {
	logger, err := zap.NewProduction()
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
	return logger.Sugar()
}

func (h *mainDeliveryHandler) Router(r *chi.Mux, isLocalCors bool) {
	if isLocalCors {
		r.Use(ownMiddleware.CORS)
	}
	r.Use(middleware.Logger)

	r.Post("/login", h.auth.Login)
	r.Post("/logout", h.auth.Logout)
	r.Post("/register", h.auth.Register)
	r.Post("/updateUserData", h.auth.UpdateUser)
	r.Get("/checkAuthorized", h.auth.CheckAuthorized)

	r.Post("/NewGame", h.game.HandleNewGame)
	r.Post("/JoinGame", h.game.HandleJoinGame)
	r.Get("/startGame", h.game.HandleStartGame)
	r.Post("/getGameByPublicKey", h.game.HandleGetGameByPublicKey)
	r.Get("/leaveGame", h.game.HandleLeaveGame)
	r.Post("/getUserById", h.auth.GetUserByID)
	r.Post("/getUserByUsername", h.auth.GetUserByUsername)
	r.Get("/getArchive", h.game.HandleGetArchivePaginator)
	r.Get("/getYearsInArchive", h.game.HandleGetYearsInArchive)
	r.Get("/getNamesInArchive", h.game.HandleGetNamesInArchive)
	r.Post("/getGameFromArchiveById", h.game.HandleGetGameFromArchiveById)
	r.Get("/storeTasksToMongoByPath", h.task.HandleStoreInMongo)
	r.Get("/getAvailableGamesForUser", h.task.HandleGetAvailableGamesForUser)
	r.Get("/markTaskAsDone", h.task.HandleMarkTaskAsDone)
	r.Get("/analyseCurrent", h.game.HandleAnalyseGame)
	r.Post("/getMoveExplanation", h.game.GetMoveExplanation)

	r.Post("/generateMove", h.game.HandleGenerateMove)
	r.Post("/newBotGame", h.game.HandleNewBotGame)

	r.Get("/swagger/*", httpSwagger.WrapHandler)
}

func initDatabaseAdapters(ctx context.Context, log *zap.SugaredLogger, cfg bootstrap.Config) *dataBaseAdapters {
	mongoAdapter := adapters.NewAdapterMongo(&cfg)
	if err := mongoAdapter.Init(ctx); err != nil {
		log.Fatal("Не удалось инициализировать MongoDB", zap.Error(err))
	}

	redisAdapter := adapters.NewAdapterRedis(&cfg)
	if err := redisAdapter.Init(ctx); err != nil {
		log.Fatal("Не удалось инициализировать Redis", zap.Error(err))
	}
	llmAdapter := adapters.NewLlmAdapter(cfg.LlmApiKey, cfg.LlmAgentKey)
	resultServerAdapter := adapters.NewResultServerAdapter(cfg.ResultServerUrl)

	log.Info("Адаптеры баз данных инициализированы")
	return &dataBaseAdapters{
		redisAdapter:        redisAdapter,
		llmAdapter:          llmAdapter,
		mongoAdapter:        mongoAdapter,
		resultServerAdapter: resultServerAdapter,
	}
}

func initializeDeliveryHandlers(
	cfg bootstrap.Config,
	log *zap.SugaredLogger,
	databaseAdapters *dataBaseAdapters,
) *mainDeliveryHandler {

	katagoRepo := internalRepository.NewKatagoStorage(&cfg, databaseAdapters.mongoAdapter, databaseAdapters.redisAdapter)
	katagoUC := katagoUseCase.NewKatagoUseCase(katagoRepo)

	authDeliveryHandler := authDelivery.NewAuthHandler(databaseAdapters.redisAdapter, databaseAdapters.mongoAdapter, log)
	gameDeliveryHandler := gameDelivery.NewGameHandler(cfg, log, databaseAdapters.mongoAdapter, databaseAdapters.redisAdapter, authDeliveryHandler, katagoUC, databaseAdapters.llmAdapter, databaseAdapters.resultServerAdapter)
	taskDeliveryHandler := taskDelivery.NewTaskHandler(log, &cfg, authDeliveryHandler, databaseAdapters.mongoAdapter)

	return &mainDeliveryHandler{
		auth: authDeliveryHandler,
		game: gameDeliveryHandler,
		task: taskDeliveryHandler,
	}
}

func handleShutdown(cancelFunc context.CancelFunc, log *zap.SugaredLogger) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Info("Received shutdown signal")
	cancelFunc()
	time.Sleep(1 * time.Second)

}
