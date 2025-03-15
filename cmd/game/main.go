package main

import (
	"TP-Game/internal/auth/delivery"
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type mainDeliveryHandler struct {
	auth *delivery.AuthHandler
}

//TODO проверять количество сессий, чтобы не абузили /login

func initializeDeliveryHandlers() *mainDeliveryHandler {
	fmt.Println("Redis env variable is: ", os.Getenv("REDIS_URL"))
	redisClient := redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_URL"),
		Password: "", // No password set
		DB:       0,  // Use default DB
	})
	_, err := redisClient.Ping(context.Background()).Result()
	if err != nil {
		panic(err)
	}
	fmt.Println("Redis connection established")
	return &mainDeliveryHandler{auth: delivery.NewMapAuthHandler(redisClient)}
}

func loadEnvFile() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
}

func main() {
	//logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	//slog.SetDefault(logger)
	loadEnvFile()
	mainDeliveryHandler := initializeDeliveryHandlers()
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Post("/login", mainDeliveryHandler.auth.Login)
	r.Delete("/logout", mainDeliveryHandler.auth.Logout)

	err := http.ListenAndServe(":8080", r)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(0)
	}
}
