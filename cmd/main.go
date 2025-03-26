package main

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	authDelivery "team_exe/internal/delivery/auth"
	gameDelivery "team_exe/internal/delivery/game"
	katagoDelivery "team_exe/internal/delivery/katago"
	ownMiddleware "team_exe/internal/middleware"
	katagoProto "team_exe/microservices/proto"
)

type mainDeliveryHandler struct {
	auth   *authDelivery.AuthHandler
	katago *katagoDelivery.KatagoHandler
	game   *gameDelivery.GameHandler
}

type dataBaseAdapters struct {
	redisAdapter *adapters.AdapterRedis
	mongoAdapter *adapters.AdapterMongo
}

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

	grpcKatago, err := grpc.Dial("host.docker.internal:8082", grpc.WithInsecure())
	if err != nil {
		logger.Fatal("Failed to dial grpc", zap.Error(err))
	}
	defer grpcKatago.Close()

	r := chi.NewRouter()
	handlers := initializeDeliveryHandlers(ctx, *cfg, logger, grpcKatago, databaseAdapters)
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
	r.Delete("/logout", h.auth.Logout)
	r.Post("/autoBotGenerateMove", h.katago.HandleGenerateMove)
	r.Post("/NewGame", h.game.HandleNewGame)
	r.Post("/JoinGame", h.game.HandleJoinGame)
	r.Get("/startGame", h.game.HandleStartGame)
	r.Post("/getGameById", h.game.GetGameById)
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

	log.Info("Адаптеры баз данных инициализированы")
	return &dataBaseAdapters{
		redisAdapter: redisAdapter,
		mongoAdapter: mongoAdapter,
	}
}

func initializeDeliveryHandlers(
	ctx context.Context,
	cfg bootstrap.Config,
	log *zap.SugaredLogger,
	grpcKatago *grpc.ClientConn,
	databaseAdapters *dataBaseAdapters,
) *mainDeliveryHandler {
	katagoManager := katagoProto.NewKatagoServiceClient(grpcKatago)
	katagoDeliveryHandler := katagoDelivery.NewKatagoHandler(cfg, log, katagoManager)

	authDeliveryHandler := authDelivery.NewMapAuthHandler(databaseAdapters.redisAdapter)
	gameDeliveryHandler := gameDelivery.NewGameHandler(cfg, log, databaseAdapters.mongoAdapter, databaseAdapters.redisAdapter, authDeliveryHandler)

	return &mainDeliveryHandler{
		auth:   authDeliveryHandler,
		katago: katagoDeliveryHandler,
		game:   gameDeliveryHandler,
	}
}

func handleShutdown(cancelFunc context.CancelFunc, log *zap.SugaredLogger) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Info("Received shutdown signal")
	cancelFunc()
	time.Sleep(1 * time.Second) // дать время закрыть соединения
}
