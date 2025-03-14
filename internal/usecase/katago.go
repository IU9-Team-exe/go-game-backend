package usecase

import "team_exe/internal/domain"

type KatagoStore interface {
}

type KatagoUseCase struct {
	store KatagoStore
}

func NewKatagoUseCase(store KatagoStore) *KatagoUseCase {
	return &KatagoUseCase{
		store: store,
	}
}

func (k *KatagoUseCase) StartGame(cfg domain.KatagoGameStartRequest) (domain.KatagoGameStartResponse, error) {

}
