package katago

import (
	"encoding/json"
	"go.uber.org/zap"
	"net/http"
	"team_exe/internal/bootstrap"
	"team_exe/internal/domain/game"
	katagoUC "team_exe/internal/usecase/katago"
	katagoProto "team_exe/microservices/proto"
)

type GenerateMoveRequest game.Moves

type BotMoveResponse struct {
	BotMove game.Move `json:"bot_move"`
}
type KatagoHandler struct {
	cfg        bootstrap.Config
	log        *zap.SugaredLogger
	katagoGRPC katagoProto.KatagoServiceClient
}

func NewKatagoHandler(cfg bootstrap.Config, log *zap.SugaredLogger, katago katagoProto.KatagoServiceClient) *KatagoHandler {
	//	repo := repository.NewKatagoRepository(&cfg, log)
	return &KatagoHandler{
		cfg:        cfg,
		log:        log,
		katagoGRPC: katago,
	}
}

func (k *KatagoHandler) HandleGenerateMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(k.log, w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	var movesToBot game.Moves
	if err := json.NewDecoder(r.Body).Decode(&movesToBot); err != nil {
		writeJSONError(k.log, w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	ctx := r.Context()

	botMove, err := katagoUC.GenMove(ctx, movesToBot, k.katagoGRPC)
	if err != nil {
		k.log.Errorf("failed to generate bot move: %v", err)
		writeJSONError(k.log, w, http.StatusInternalServerError, "Failed to generate bot move")
		return
	}

	resp := BotMoveResponse{BotMove: botMove}

	writeJSON(k.log, w, http.StatusOK, resp)
}

func writeJSON(log *zap.SugaredLogger, w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Errorf("writeJSON encode error: %v", err)
	}
}

func writeJSONError(log *zap.SugaredLogger, w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	log.Debugf("writeJSONError: %s", msg)
}
