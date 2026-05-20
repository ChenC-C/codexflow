package store

import "strings"

type LocalStateDB struct {
}

type PersistedSessionState struct {
	Ended   bool
	Managed bool
}

func OpenLocalStateDB(path string) (*LocalStateDB, error) {
	// Temporary no-op state store: this lets the agent run without downloading
	// the SQLite driver in restricted-network environments.
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	return &LocalStateDB{}, nil
}

func (db *LocalStateDB) LoadSessionStates() (map[string]PersistedSessionState, error) {
	return map[string]PersistedSessionState{}, nil
}

func (db *LocalStateDB) SaveSessionState(threadID string, state PersistedSessionState) error {
	return nil
}
