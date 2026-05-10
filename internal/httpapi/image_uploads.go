package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxUploadImageBytes = 15 * 1024 * 1024
	imageTTL            = 24 * time.Hour
)

type imageUpload struct {
	ID        string
	Name      string
	Path      string
	Size      int64
	CreatedAt time.Time
}

type imageUploadStore struct {
	mu      sync.Mutex
	baseDir string
	items   map[string]imageUpload
}

func newImageUploadStore() *imageUploadStore {
	return &imageUploadStore{
		baseDir: filepath.Join(os.TempDir(), "codexflow", "uploads"),
		items:   make(map[string]imageUpload),
	}
}

func (s *imageUploadStore) Save(name string, payload []byte) (imageUpload, error) {
	if len(payload) == 0 {
		return imageUpload{}, errors.New("empty image payload")
	}
	if len(payload) > maxUploadImageBytes {
		return imageUpload{}, errors.New("image exceeds 15MB size limit")
	}
	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return imageUpload{}, err
	}

	id, err := randomUploadID()
	if err != nil {
		return imageUpload{}, err
	}

	ext := normalizeExt(filepath.Ext(strings.TrimSpace(name)))
	if ext == "" {
		ext = ".bin"
	}
	fileName := id + ext
	path := filepath.Join(s.baseDir, fileName)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return imageUpload{}, err
	}

	item := imageUpload{
		ID:        id,
		Name:      strings.TrimSpace(name),
		Path:      path,
		Size:      int64(len(payload)),
		CreatedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	s.items[id] = item
	s.cleanupLocked(time.Now().UTC())
	s.mu.Unlock()

	return item, nil
}

func (s *imageUploadStore) Resolve(uploadID string) (string, error) {
	id := strings.TrimSpace(uploadID)
	if id == "" {
		return "", errors.New("upload id is required")
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	item, ok := s.items[id]
	if !ok {
		return "", errors.New("uploaded image not found or expired")
	}
	if _, err := os.Stat(item.Path); err != nil {
		delete(s.items, id)
		return "", errors.New("uploaded image is unavailable")
	}
	return item.Path, nil
}

func (s *imageUploadStore) cleanupLocked(now time.Time) {
	for id, item := range s.items {
		if now.Sub(item.CreatedAt) <= imageTTL {
			continue
		}
		_ = os.Remove(item.Path)
		delete(s.items, id)
	}
}

func randomUploadID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func normalizeExt(ext string) string {
	trimmed := strings.TrimSpace(strings.ToLower(ext))
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, ".") {
		return trimmed
	}
	return "." + trimmed
}
