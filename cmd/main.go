package main

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"net/http"
	"os"
	"team_exe/internal/bootstrap"
	authDelivery "team_exe/internal/delivery/auth"
	katagoDelivery "team_exe/internal/delivery/katago"
	katagoProto "team_exe/microservices/proto"
)

type mainDeliveryHandler struct {
	auth   *authDelivery.AuthHandler
	katago *katagoDelivery.KatagoHandler
}

type databases struct {
	redisClient *redis.Client
}

func main() {
	logger := NewLogger()
	cfg, err := bootstrap.Setup(".env")
	if err != nil {
		logger.Error("Failed to setup configuration", zap.Error(err))
		return
	}

	r := chi.NewRouter()

	grpcKatago, err := grpc.Dial(
		"host.docker.internal:8082",
		grpc.WithInsecure(),
	)
	if err != nil {
		logger.Fatal("Failed to dial grpc", zap.Error(err))
	}
	defer grpcKatago.Close()

	mainDeliveryHandlers := initializeDeliveryHandlers(*cfg, logger, grpcKatago)
	mainDeliveryHandlers.Router(r)

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

func (h *mainDeliveryHandler) Router(r *chi.Mux) {
	r.Use(middleware.Logger)

	r.Post("/login", h.auth.Login)
	r.Delete("/logout", h.auth.Logout)
	r.Post("/autoBotGenerateMove", h.katago.HandleGenerateMove)
}

func initRedis(log *zap.SugaredLogger, cfg bootstrap.Config) *redis.Client {
	redisURL := os.Getenv("REDIS_URL")
	log.Info("Connecting to Redis at:", redisURL)

	client := redis.NewClient(&redis.Options{
		Addr:     redisURL,
		Password: "",
		DB:       0,
	})

	if _, err := client.Ping(context.Background()).Result(); err != nil {
		log.Fatal("failed to connect Redis", zap.Error(err))
	}

	log.Info("Redis connection established")
	return client
}

func initDatabase(log *zap.SugaredLogger, cfg bootstrap.Config) *databases {
	return &databases{
		redisClient: initRedis(log, cfg),
	}
}

func initializeDeliveryHandlers(cfg bootstrap.Config, log *zap.SugaredLogger, grpcKatago *grpc.ClientConn) *mainDeliveryHandler {
	databaseStorage := initDatabase(log, cfg)

	katagoManager := katagoProto.NewKatagoServiceClient(grpcKatago)
	katagoDeliveryHandler := katagoDelivery.NewKatagoHandler(cfg, log, katagoManager)

	authDeliveryHandler := authDelivery.NewMapAuthHandler(databaseStorage.redisClient)

	return &mainDeliveryHandler{
		auth:   authDeliveryHandler,
		katago: katagoDeliveryHandler,
	}
}
