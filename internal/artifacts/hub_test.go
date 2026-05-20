package artifacts

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHubListsAllowedArtifactsAndResolvesTextReferences(t *testing.T) {
	root := t.TempDir()
	mobileDir := filepath.Join(root, "mobile-download")
	if err := os.MkdirAll(mobileDir, 0o755); err != nil {
		t.Fatalf("create mobile dir: %v", err)
	}
	pdfPath := filepath.Join(root, "report.pdf")
	apkPath := filepath.Join(mobileDir, "codexflow.apk")
	ignoredPath := filepath.Join(root, "notes.txt")
	for path, payload := range map[string]string{
		pdfPath:     "%PDF-1.4\n",
		apkPath:     "apk",
		ignoredPath: "ignore",
	} {
		if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	hub := NewHub([]Root{
		{Path: root, Recursive: false},
		{Path: mobileDir, Recursive: true},
	})

	items := hub.List()
	if got, want := len(items), 2; got != want {
		t.Fatalf("artifacts count = %d, want %d: %#v", got, want, items)
	}

	refs := hub.ResolveTextArtifacts("产物在 "+pdfPath+" 以及 mobile-download/codexflow.apk", root)
	if got, want := len(refs), 2; got != want {
		t.Fatalf("resolved refs = %d, want %d: %#v", got, want, refs)
	}
	if refs[0].SourceText == "" || refs[1].SourceText == "" {
		t.Fatalf("resolved refs should include source text: %#v", refs)
	}
}

func TestHubResolvesMarkdownLinkArtifactSourceText(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "preview.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	hub := NewHub([]Root{{Path: root, Recursive: false}})

	refs := hub.ResolveTextArtifacts("图片在 [preview.png](preview.png)，请直接打开。", root)
	if got, want := len(refs), 1; got != want {
		t.Fatalf("resolved refs = %d, want %d: %#v", got, want, refs)
	}
	if got, want := refs[0].SourceText, "preview.png"; got != want {
		t.Fatalf("source text = %q, want %q", got, want)
	}
}

func TestHubServePreviewUsesInlineDisposition(t *testing.T) {
	root := t.TempDir()
	pdfPath := filepath.Join(root, "report.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	hub := NewHub([]Root{{Path: root, Recursive: false}})
	items := hub.List()
	if len(items) != 1 {
		t.Fatalf("artifacts count = %d, want 1", len(items))
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/artifacts/"+items[0].ID+"/preview", nil)
	if err := hub.Serve(recorder, request, items[0].ID, true); err != nil {
		t.Fatalf("serve preview: %v", err)
	}
	if disposition := recorder.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "inline") {
		t.Fatalf("content disposition = %q, want inline", disposition)
	}
}

func TestHubSkipsSymlinkEscapes(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.pdf")
	if err := os.WriteFile(outside, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write outside pdf: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "secret.pdf")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	hub := NewHub([]Root{{Path: root, Recursive: true}})
	if got := len(hub.List()); got != 0 {
		t.Fatalf("artifacts count = %d, want 0 for symlink escape", got)
	}
}
