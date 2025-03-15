package repository

import (
	"github.com/google/uuid"
	"go.uber.org/zap"
	"os"
	"sync"

	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
)

type KatagoRepository struct {
	cfg       *bootstrap.Config
	log       *zap.SugaredLogger
	redis     *adapters.AdapterRedis
	mongo     *adapters.AdapterMongo
	kataGoURL string     // URL сервера, где крутится KataGo (например "http://1.2.3.4:8080")
	mu        sync.Mutex // если нужно защищать какие-то общие ресурсы
}

func NewKatagoRepository(cfg *bootstrap.Config, log *zap.SugaredLogger) (*KatagoRepository, error) {
	// Можно URL KataGo брать из cfg (config.yml), а можно захардкодить
	kataGoURL := os.Getenv("KATAGO_URL")
	if kataGoURL == "" {
		kataGoURL = "http://127.0.0.1:8080" // пример
	}

	return &KatagoRepository{
		cfg:       cfg,
		log:       log,
		redis:     adapters.NewAdapterRedis(cfg),
		mongo:     adapters.NewAdapterMongo(cfg),
		kataGoURL: kataGoURL,
	}, nil
}

func generateUUID() string {
	return uuid.New().String()
}
