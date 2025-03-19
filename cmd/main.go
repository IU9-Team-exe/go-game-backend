package main

import (
	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"net/http"
	"team_exe/internal/bootstrap"
	"team_exe/internal/delivery"
	"team_exe/internal/repository"
)

func main() {
	r := mux.NewRouter()

	logger := NewLogger()
	cfg, err := bootstrap.Setup(".env")
	if err != nil {
		logger.Error("Failed to setup configuration", zap.Error(err))
		return
	}

	katagoRepo := repository.NewKatagoRepository(cfg, logger)
	katagoDel := delivery.NewKatago(*cfg, logger, katagoRepo)

	r.HandleFunc("/autoBotGenerateMove", katagoDel.HandleGenerateMove).Methods("POST")

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
