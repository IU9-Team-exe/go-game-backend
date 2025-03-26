package repo

import "team_exe/internal/domain/user"

type UserMapStorage struct {
	users map[int]user.User
}

func NewMapUserStorage() *UserMapStorage {
	storage := &UserMapStorage{users: make(map[int]user.User)}
	storage.users[5] = user.User{
		ID:           "5",
		Username:     "artem",
		PasswordHash: "755",
		PasswordSalt: "",
	}

	storage.users[4] = user.User{
		ID:           "4",
		Username:     "FunnyRockfish",
		PasswordHash: "770",
		PasswordSalt: "",
	}
	return storage
}

func (u UserMapStorage) CheckExists(username string) bool {
	for _, v := range u.users {
		if v.Username == username {
			return true
		}
	}
	return false
}

func (u UserMapStorage) GetUser(username string) (user.User, bool) {
	for _, v := range u.users {
		if v.Username == username {
			return v, true
		}
	}
	return user.User{}, false
}

func (u UserMapStorage) GetUserByID(id string) (user.User, bool) {
	for _, v := range u.users {
		if v.ID == id {
			return v, true
		}
	}
	return user.User{}, false
}

type SessionMapStorage struct {
	sessions map[string]string
	users    map[string]string
}

func (u SessionMapStorage) DeleteSession(sessionID string) (ok bool) {
	_, found := u.sessions[sessionID]
	if !found {
		return false
	}
	delete(u.sessions, sessionID)
	return true
}

func NewSessionMapStorage() *SessionMapStorage {
	return &SessionMapStorage{
		sessions: make(map[string]string),
		users:    make(map[string]string),
	}
}

func (u SessionMapStorage) GetUserIdBySession(sessionID string) (string, bool) {
	if v, ok := u.sessions[sessionID]; ok {
		return v, true
	} else {
		return "", false
	}
}

func (u SessionMapStorage) StoreSession(sessionID string, userID string) {
	u.sessions[sessionID] = userID
	u.users[userID] = sessionID
	return
}
