package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFileUploadAcceptsNonImageMultipartFiles(t *testing.T) {
	t.Parallel()

	server := &Server{
		mux:     http.NewServeMux(),
		uploads: newImageUploadStore(),
	}
	server.uploads.baseDir = t.TempDir()
	server.routes()

	body, contentType := multipartBody(t, "file", "notes.txt", []byte("plain text attachment"))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/uploads/file", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	server.mux.ServeHTTP(response, request)

	if got, want := response.Code, http.StatusCreated; got != want {
		t.Fatalf("upload status = %d, want %d; body=%s", got, want, response.Body.String())
	}

	var payload struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if payload.ID == "" {
		t.Fatal("upload response id is empty")
	}
	if got, want := payload.Name, "notes.txt"; got != want {
		t.Fatalf("upload name = %q, want %q", got, want)
	}
	if got, want := payload.Size, int64(len("plain text attachment")); got != want {
		t.Fatalf("upload size = %d, want %d", got, want)
	}

	path, err := server.uploads.Resolve(payload.ID)
	if err != nil {
		t.Fatalf("resolve uploaded file: %v", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stored upload: %v", err)
	}
	if got, want := string(stored), "plain text attachment"; got != want {
		t.Fatalf("stored payload = %q, want %q", got, want)
	}
}

func TestBuildTurnInputConvertsUploadedFileToTextPath(t *testing.T) {
	t.Parallel()

	server := &Server{uploads: newImageUploadStore()}
	server.uploads.baseDir = t.TempDir()

	item, err := server.uploads.Save("notes.txt", []byte("plain text attachment"))
	if err != nil {
		t.Fatalf("save upload: %v", err)
	}

	input, err := server.buildTurnInput("", []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		UploadID string `json:"uploadId"`
	}{
		{Type: "file", UploadID: item.ID},
	})
	if err != nil {
		t.Fatalf("build turn input: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("input count = %d, want 1", len(input))
	}
	if got, want := input[0]["type"], "text"; got != want {
		t.Fatalf("input type = %v, want %q", got, want)
	}
	text, ok := input[0]["text"].(string)
	if !ok {
		t.Fatalf("input text has type %T, want string", input[0]["text"])
	}
	if !strings.Contains(text, item.Path) {
		t.Fatalf("input text %q does not include uploaded path %q", text, item.Path)
	}
}

func multipartBody(t *testing.T, field, name string, payload []byte) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}
