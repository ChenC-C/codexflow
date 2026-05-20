package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Root struct {
	Path      string
	Recursive bool
	Label     string
}

type Artifact struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	MimeType    string    `json:"mimeType"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"modTime"`
	DownloadURL string    `json:"downloadUrl"`
	PreviewURL  string    `json:"previewUrl"`

	absPath string
}

type Ref struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	MimeType    string `json:"mimeType"`
	DownloadURL string `json:"downloadUrl"`
	PreviewURL  string `json:"previewUrl"`
	SourceText  string `json:"sourceText,omitempty"`
}

type Hub struct {
	mu       sync.RWMutex
	roots    []Root
	byID     map[string]Artifact
	byPath   map[string]Artifact
	byName   map[string][]Artifact
	byRel    map[string]Artifact
	lastScan time.Time
}

var allowedExtensions = map[string]struct {
	kind string
	mime string
}{
	".pdf":  {kind: "pdf", mime: "application/pdf"},
	".png":  {kind: "image", mime: "image/png"},
	".jpg":  {kind: "image", mime: "image/jpeg"},
	".jpeg": {kind: "image", mime: "image/jpeg"},
	".gif":  {kind: "image", mime: "image/gif"},
	".webp": {kind: "image", mime: "image/webp"},
	".bmp":  {kind: "image", mime: "image/bmp"},
	".apk":  {kind: "apk", mime: "application/vnd.android.package-archive"},
	".zip":  {kind: "archive", mime: "application/zip"},
	".tar":  {kind: "archive", mime: "application/x-tar"},
	".gz":   {kind: "archive", mime: "application/gzip"},
	".tgz":  {kind: "archive", mime: "application/gzip"},
	".7z":   {kind: "archive", mime: "application/x-7z-compressed"},
}

func NewHub(roots []Root) *Hub {
	hub := &Hub{
		roots:  normalizeRoots(roots),
		byID:   make(map[string]Artifact),
		byPath: make(map[string]Artifact),
		byName: make(map[string][]Artifact),
		byRel:  make(map[string]Artifact),
	}
	_ = hub.Refresh()
	return hub
}

func DefaultRoots() []Root {
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		wd = "."
	}

	roots := []Root{
		{Path: wd, Recursive: false, Label: "workspace"},
		{Path: filepath.Join(wd, "mobile-download"), Recursive: true, Label: "mobile-download"},
	}

	parent := filepath.Dir(wd)
	if parent != wd && filepath.Base(wd) == "codexflow-main" {
		roots = append(roots,
			Root{Path: parent, Recursive: false, Label: "workspace"},
			Root{Path: filepath.Join(parent, "mobile-download"), Recursive: true, Label: "mobile-download"},
		)
	}

	return roots
}

func (h *Hub) Refresh() error {
	items, err := h.scan()
	if err != nil {
		return err
	}

	byID := make(map[string]Artifact, len(items))
	byPath := make(map[string]Artifact, len(items))
	byName := make(map[string][]Artifact, len(items))
	byRel := make(map[string]Artifact, len(items))
	for _, item := range items {
		byID[item.ID] = item
		byPath[item.absPath] = item
		byName[strings.ToLower(item.Name)] = append(byName[strings.ToLower(item.Name)], item)
		byRel[filepath.ToSlash(strings.ToLower(item.Path))] = item
	}

	h.mu.Lock()
	h.byID = byID
	h.byPath = byPath
	h.byName = byName
	h.byRel = byRel
	h.lastScan = time.Now().UTC()
	h.mu.Unlock()
	return nil
}

func (h *Hub) List() []Artifact {
	_ = h.Refresh()
	h.mu.RLock()
	defer h.mu.RUnlock()

	items := make([]Artifact, 0, len(h.byID))
	for _, item := range h.byID {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ModTime.Equal(items[j].ModTime) {
			return items[i].Path < items[j].Path
		}
		return items[i].ModTime.After(items[j].ModTime)
	})
	return items
}

func (h *Hub) ResolveTextArtifacts(text, cwd string) []Ref {
	_ = h.Refresh()
	candidates := candidateTokens(text)
	if len(candidates) == 0 {
		return nil
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	refs := make([]Ref, 0)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		for _, item := range h.resolveTokenLocked(candidate.Cleaned, cwd) {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			ref := toRef(item)
			ref.SourceText = candidate.Cleaned
			refs = append(refs, ref)
		}
	}
	return refs
}

func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, id string, preview bool) error {
	_ = h.Refresh()
	h.mu.RLock()
	item, ok := h.byID[strings.TrimSpace(id)]
	h.mu.RUnlock()
	if !ok {
		return errors.New("artifact not found")
	}

	if !h.pathAllowed(item.absPath) {
		return errors.New("artifact path is outside allowed roots")
	}

	file, err := os.Open(item.absPath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	disposition := "attachment"
	if preview && isPreviewableKind(item.Kind) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", item.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, item.Name))
	w.Header().Set("X-CodexFlow-Artifact-Name", item.Name)
	http.ServeContent(w, r, item.Name, stat.ModTime(), file)
	return nil
}

func (h *Hub) scan() ([]Artifact, error) {
	items := make([]Artifact, 0)
	for _, root := range h.roots {
		rootPath, err := filepath.Abs(root.Path)
		if err != nil {
			continue
		}
		info, err := os.Stat(rootPath)
		if err != nil || !info.IsDir() {
			continue
		}

		if !root.Recursive {
			entries, err := os.ReadDir(rootPath)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
					continue
				}
				if item, ok := h.buildArtifact(rootPath, filepath.Join(rootPath, entry.Name())); ok {
					items = append(items, item)
				}
			}
			continue
		}

		_ = filepath.WalkDir(rootPath, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if item, ok := h.buildArtifact(rootPath, path); ok {
				items = append(items, item)
			}
			return nil
		})
	}
	return items, nil
}

func (h *Hub) buildArtifact(rootPath, path string) (Artifact, bool) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Artifact{}, false
	}
	if !isAllowedPath(absPath) {
		return Artifact{}, false
	}
	if !withinRoot(absPath, rootPath) {
		return Artifact{}, false
	}

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return Artifact{}, false
	}

	metadata := allowedExtensions[strings.ToLower(filepath.Ext(absPath))]
	rel, err := filepath.Rel(rootPath, absPath)
	if err != nil {
		rel = filepath.Base(absPath)
	}
	display := filepath.ToSlash(rel)
	if filepath.Base(rootPath) == "mobile-download" {
		display = filepath.ToSlash(filepath.Join("mobile-download", rel))
	}

	id := stableID(absPath)
	return Artifact{
		ID:          id,
		Name:        filepath.Base(absPath),
		Path:        display,
		Kind:        metadata.kind,
		MimeType:    mimeTypeFor(absPath, metadata.mime),
		Size:        info.Size(),
		ModTime:     info.ModTime(),
		DownloadURL: "/api/v1/artifacts/" + id,
		PreviewURL:  "/api/v1/artifacts/" + id + "/preview",
		absPath:     absPath,
	}, true
}

func (h *Hub) resolveTokenLocked(token, cwd string) []Artifact {
	cleaned := cleanToken(token)
	if cleaned == "" || !isAllowedPath(cleaned) {
		return nil
	}

	candidates := make([]string, 0, 4)
	if filepath.IsAbs(cleaned) {
		candidates = append(candidates, cleaned)
	} else {
		if strings.TrimSpace(cwd) != "" {
			candidates = append(candidates, filepath.Join(cwd, cleaned))
		}
		for _, root := range h.roots {
			candidates = append(candidates, filepath.Join(root.Path, cleaned))
			if filepath.Base(root.Path) == "mobile-download" && strings.HasPrefix(filepath.ToSlash(cleaned), "mobile-download/") {
				candidates = append(candidates, filepath.Join(filepath.Dir(root.Path), cleaned))
			}
		}
	}

	result := make([]Artifact, 0, 1)
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if item, ok := h.byPath[abs]; ok {
			result = append(result, item)
		}
	}
	if len(result) > 0 {
		return result
	}

	if item, ok := h.byRel[filepath.ToSlash(strings.ToLower(cleaned))]; ok {
		return []Artifact{item}
	}
	if byName := h.byName[strings.ToLower(filepath.Base(cleaned))]; len(byName) == 1 {
		return byName
	}
	return nil
}

func (h *Hub) pathAllowed(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil || !isAllowedPath(abs) {
		return false
	}
	for _, root := range h.roots {
		rootPath, err := filepath.Abs(root.Path)
		if err != nil {
			continue
		}
		if withinRoot(abs, rootPath) {
			return true
		}
	}
	return false
}

func normalizeRoots(roots []Root) []Root {
	seen := make(map[string]struct{}, len(roots))
	result := make([]Root, 0, len(roots))
	for _, root := range roots {
		path := strings.TrimSpace(root.Path)
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%s:%t", abs, root.Recursive)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		root.Path = abs
		result = append(result, root)
	}
	return result
}

type textCandidate struct {
	Raw     string
	Cleaned string
}

func candidateTokens(text string) []textCandidate {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || r == '\'' || r == '`' || r == '<' || r == '>' || r == '[' || r == ']'
	})
	result := make([]textCandidate, 0, len(fields))
	for _, field := range fields {
		cleaned := cleanToken(field)
		if cleaned != "" && isAllowedPath(cleaned) {
			result = append(result, textCandidate{Raw: field, Cleaned: cleaned})
		}
	}
	return result
}

func cleanToken(value string) string {
	return strings.Trim(strings.TrimSpace(value), ".,;:，。；：)）(（")
}

func isAllowedPath(path string) bool {
	_, ok := allowedExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func withinRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func mimeTypeFor(path, fallback string) string {
	if value := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); value != "" {
		return value
	}
	return fallback
}

func stableID(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:])[:20]
}

func isPreviewableKind(kind string) bool {
	return kind == "pdf" || kind == "image"
}

func toRef(item Artifact) Ref {
	return Ref{
		ID:          item.ID,
		Name:        item.Name,
		Path:        item.Path,
		Kind:        item.Kind,
		MimeType:    item.MimeType,
		DownloadURL: item.DownloadURL,
		PreviewURL:  item.PreviewURL,
	}
}
