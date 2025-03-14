package delivery

import (
	"encoding/json"
	"go.uber.org/zap"
	"io"
	"net/http"
	"team_exe/internal/bootstrap"
	"team_exe/internal/domain"
)

type Katago struct {
	cfg bootstrap.Config
	log *zap.SugaredLogger
}

func NewKatago(cfg bootstrap.Config, log *zap.SugaredLogger) *Katago {
	return &Katago{
		cfg: cfg,
		log: log,
	}
}

func (k *Katago) SendRequest(w http.ResponseWriter, r *http.Request) {

}

func (k *Katago) StartGame(w http.ResponseWriter, r *http.Request) {
	var startCfg domain.KatagoGameStartRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		k.log.Error(err)
		return
	}
	r.Body.Close()

	err = json.Unmarshal(body, &startCfg)
	if err != nil {
		k.log.Error(err)
		return
	}
	k.log.Info("конфиг игры: ", startCfg)
}
