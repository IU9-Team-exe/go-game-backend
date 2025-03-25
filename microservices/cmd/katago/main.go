package main

import (
	"fmt"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"log"
	"net"
	"team_exe/internal/bootstrap"
	katago "team_exe/microservices/proto"
	"team_exe/microservices/repository"
	"team_exe/microservices/usecase"
)

func main() {
	logger := NewLogger()
	cfg, err := bootstrap.Setup(".env")
	if err != nil {
		logger.Error("Failed to setup configuration", zap.Error(err))
		return
	}

	lis, err := net.Listen("tcp", ":8082")
	if err != nil {
		log.Fatalln("cant listen port", err)
	}

	server := grpc.NewServer()
	katagoStorage := repository.NewKatagoRepository(cfg, logger)
	katago.RegisterKatagoServiceServer(server, usecase.NewKatagoUseCase(katagoStorage))
	fmt.Println("starting server at :8082")
	server.Serve(lis)
}

func NewLogger() *zap.SugaredLogger {
	logger, err := zap.NewProduction()
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}

	return logger.Sugar()
}
