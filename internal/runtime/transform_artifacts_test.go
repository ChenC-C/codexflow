package runtime

import (
	"strings"
	"testing"

	"codexflow/internal/artifacts"
)

type fakeArtifactResolver struct{}

func (fakeArtifactResolver) ResolveTextArtifacts(text, cwd string) []artifacts.Ref {
	if !strings.Contains(text, "report.pdf") {
		return nil
	}
	return []artifacts.Ref{{
		ID:          "artifact-1",
		Name:        "report.pdf",
		Path:        "report.pdf",
		Kind:        "pdf",
		MimeType:    "application/pdf",
		DownloadURL: "/api/v1/artifacts/artifact-1",
		PreviewURL:  "/api/v1/artifacts/artifact-1/preview",
		SourceText:  "report.pdf",
	}}
}

func TestNormalizeItemAttachesArtifactRefs(t *testing.T) {
	item := normalizeItem(map[string]any{
		"type": "agentMessage",
		"text": "已生成 /tmp/report.pdf",
	}, fakeArtifactResolver{}, "/tmp")

	if got, want := len(item.Artifacts), 1; got != want {
		t.Fatalf("artifacts count = %d, want %d", got, want)
	}
	if item.Artifacts[0].PreviewURL == "" {
		t.Fatalf("preview url should be populated")
	}
}
