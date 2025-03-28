package errors

import "errors"

var (
	ErrUserNotFound     = errors.New("user with provided username was not found")
	ErrWrongPassword    = errors.New("wrong password")
	ErrSessionNotFound  = errors.New("session was not found")
	ErrCreateGameFailed = errors.New("create game failed")
	ErrJoinGameFailed   = errors.New("join game failed")
	ErrGameNotFound     = errors.New("game not found")
	ErrUserExists       = errors.New("user already exists")
	ErrInternal         = errors.New("internal error")
)
