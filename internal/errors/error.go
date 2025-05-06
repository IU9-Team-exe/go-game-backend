package errors

import (
	"errors"
	"net/http"
)

var (
	ErrUserNotFound           = errors.New("user with provided username was not found")
	ErrWrongPassword          = errors.New("wrong password")
	ErrSessionNotFound        = errors.New("session was not found")
	ErrCreateGameFailed       = errors.New("create game failed")
	ErrJoinGameFailed         = errors.New("join game failed")
	ErrGameNotFound           = errors.New("game not found")
	ErrUserExists             = errors.New("user already exists")
	ErrInternal               = errors.New("internal error")
	ErrUserAlreadyInGame      = errors.New("user already in game")
	ErrGameKeyGeneration      = errors.New("game key generation failed")
	ErrCheckKeyUniq           = errors.New("check public key uniqueness failed")
	ErrGameInsert             = errors.New("insert game failed")
	ErrAddPlayer              = errors.New("add player failed")
	ErrNoFreeColor            = errors.New("no free color")
	ErrGameLookup             = errors.New("lookup game by public key failed")
	ErrLeaveGame              = errors.New("leave game failed")
	ErrInvalidGameKey         = errors.New("invalid game key")
	ErrGameArchiveNotFound    = errors.New("game not found in archive")
	ErrGameArchiveLookup      = errors.New("lookup game in archive failed")
	ErrSGFNotFound            = errors.New("sgf not found in redis")
	ErrRedis                  = errors.New("redis operation failed")
	ErrGetAllActiveGames      = errors.New("fetch active games failed")
	ErrDecodeGame             = errors.New("decode game failed")
	ErrMongo                  = errors.New("mongodb operation failed")
	ErrArchiveCount           = errors.New("count archive games failed")
	ErrArchiveFetch           = errors.New("fetch archive games failed")
	ErrArchiveDecode          = errors.New("decode archive games failed")
	ErrArchiveAggregate       = errors.New("aggregate archive years/names failed")
	ErrArchiveAggregateDecode = errors.New("decode archive aggregate result failed")
	ErrInvalidArchiveID       = errors.New("invalid archive game id")
	ErrArchiveNotFound        = errors.New("archive game not found")
	ErrGetArchiveByID         = errors.New("fetch archive game by id failed")
	ErrCompleteGame           = errors.New("complete game failed")
	ErrRedisDel               = errors.New("redis delete failed")
	ErrSaveSGF                = errors.New("save sgf to mongo failed")
	ErrSaveMoves              = errors.New("save moves to mongo failed")
	ErrMarshal                = errors.New("error marshalling")
	ErrSendRequest            = errors.New("error sending request")
)

func TranslateErr(err error) (status int, code string) {
	switch {
	case errors.Is(err, ErrUserNotFound):
		return http.StatusNotFound, "user_not_found"
	case errors.Is(err, ErrWrongPassword):
		return http.StatusUnauthorized, "wrong_password"
	case errors.Is(err, ErrSessionNotFound):
		return http.StatusUnauthorized, "session_not_found"
	case errors.Is(err, ErrUserExists):
		return http.StatusConflict, "user_already_exists"

	case errors.Is(err, ErrCreateGameFailed):
		return http.StatusInternalServerError, "create_game_failed"
	case errors.Is(err, ErrJoinGameFailed):
		return http.StatusInternalServerError, "join_game_failed"
	case errors.Is(err, ErrGameNotFound):
		return http.StatusNotFound, "game_not_found"
	case errors.Is(err, ErrUserAlreadyInGame):
		return http.StatusConflict, "user_already_in_game"

	case errors.Is(err, ErrGameKeyGeneration):
		return http.StatusInternalServerError, "game_key_generation_failed"
	case errors.Is(err, ErrCheckKeyUniq):
		return http.StatusInternalServerError, "public_key_uniqueness_check_failed"
	case errors.Is(err, ErrGameInsert):
		return http.StatusInternalServerError, "insert_game_failed"
	case errors.Is(err, ErrAddPlayer):
		return http.StatusInternalServerError, "add_player_failed"
	case errors.Is(err, ErrNoFreeColor):
		return http.StatusConflict, "no_free_color"
	case errors.Is(err, ErrGameLookup):
		return http.StatusInternalServerError, "lookup_game_failed"
	case errors.Is(err, ErrLeaveGame):
		return http.StatusInternalServerError, "leave_game_failed"
	case errors.Is(err, ErrInvalidGameKey):
		return http.StatusBadRequest, "invalid_game_key"

	case errors.Is(err, ErrGameArchiveNotFound):
		return http.StatusNotFound, "archive_game_not_found"
	case errors.Is(err, ErrGameArchiveLookup):
		return http.StatusInternalServerError, "archive_lookup_failed"

	case errors.Is(err, ErrSGFNotFound):
		return http.StatusNotFound, "sgf_not_found"
	case errors.Is(err, ErrRedis):
		return http.StatusBadGateway, "redis_error"

	case errors.Is(err, ErrGetAllActiveGames):
		return http.StatusInternalServerError, "fetch_active_games_failed"
	case errors.Is(err, ErrDecodeGame):
		return http.StatusInternalServerError, "decode_game_failed"
	case errors.Is(err, ErrMongo):
		return http.StatusBadGateway, "mongodb_error"

	case errors.Is(err, ErrArchiveCount):
		return http.StatusInternalServerError, "archive_count_failed"
	case errors.Is(err, ErrArchiveFetch):
		return http.StatusInternalServerError, "archive_fetch_failed"
	case errors.Is(err, ErrArchiveDecode):
		return http.StatusInternalServerError, "archive_decode_failed"
	case errors.Is(err, ErrArchiveAggregate):
		return http.StatusInternalServerError, "archive_aggregate_failed"
	case errors.Is(err, ErrArchiveAggregateDecode):
		return http.StatusInternalServerError, "archive_aggregate_decode_failed"
	case errors.Is(err, ErrInvalidArchiveID):
		return http.StatusBadRequest, "invalid_archive_id"
	case errors.Is(err, ErrArchiveNotFound):
		return http.StatusNotFound, "archive_not_found"
	case errors.Is(err, ErrGetArchiveByID):
		return http.StatusInternalServerError, "archive_fetch_by_id_failed"

	case errors.Is(err, ErrCompleteGame):
		return http.StatusInternalServerError, "complete_game_failed"
	case errors.Is(err, ErrRedisDel):
		return http.StatusBadGateway, "redis_delete_failed"
	case errors.Is(err, ErrSaveSGF):
		return http.StatusInternalServerError, "save_sgf_failed"
	case errors.Is(err, ErrSaveMoves):
		return http.StatusInternalServerError, "save_moves_failed"

	case errors.Is(err, ErrMarshal):
		return http.StatusInternalServerError, "marshalling_error"
	case errors.Is(err, ErrSendRequest):
		return http.StatusBadGateway, "external_request_failed"

	case errors.Is(err, ErrInternal):
		return http.StatusInternalServerError, "internal_error"

	default:
		return http.StatusInternalServerError, "internal_error"
	}
}
