package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
)

type libraryTestStorage struct {
	stubAssets
	objects    map[string][]byte
	failDelete bool
}

func (s *libraryTestStorage) SavePromptImage(_ context.Context, r io.Reader, promptID, imageID string) (storage.Asset, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return storage.Asset{}, err
	}
	key := "prompt-library/" + promptID + "/" + imageID
	s.objects[key] = data
	return storage.Asset{Key: key, Kind: storage.Image}, nil
}
func (s *libraryTestStorage) Read(_ context.Context, key, _ string) (*storage.Content, error) {
	data, exists := s.objects[key]
	if !exists {
		return nil, &storage.Error{Status: 404, Code: "ASSET_NOT_FOUND", Message: "文件不存在。"}
	}
	return &storage.Content{Body: io.NopCloser(bytes.NewReader(data)), Status: 200, Bytes: int64(len(data)), ContentType: "image/png"}, nil
}
func (s *libraryTestStorage) DeletePromptImage(_ context.Context, key string) error {
	if s.failDelete {
		return errors.New("simulated storage outage")
	}
	delete(s.objects, key)
	return nil
}

func binaryLibraryRequest(h http.Handler, client *authClient, path string, data []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PUT", path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	if client != nil {
		req.AddCookie(client.cookie)
		req.Header.Set("X-CSRF-Token", client.session.CSRFToken)
		req.Header.Set("X-Session-User", client.session.User.ID)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestPromptLibraryPublicReadAdminWritesAndObjectCleanup(t *testing.T) {
	db := authDB(t)
	media := &libraryTestStorage{objects: map[string][]byte{}}
	h := NewRouter(nil, media, db)
	admin := adminSignIn(t, h, db)
	user := signIn(t, h, "reader@example.test", "regular password")
	id, firstImage, secondImage := identity.ID(), identity.ID(), identity.ID()
	body := map[string]string{"id": id, "title": "雨夜街景", "content": "霓虹灯下的雨夜"}
	assertStatus(t, authRequest(h, nil, "GET", "/api/prompts", nil, nil), 200)
	assertStatus(t, authRequest(h, nil, "POST", "/api/admin/prompts", body, nil), 401)
	assertStatus(t, authRequest(h, user, "POST", "/api/admin/prompts", body, nil), 403)
	assertStatus(t, authRequest(h, admin, "POST", "/api/admin/prompts", body, map[string]string{"X-CSRF-Token": ""}), 403)
	assertStatus(t, authRequest(h, admin, "POST", "/api/admin/prompts", body, nil), 201)
	assertStatus(t, authRequest(h, admin, "POST", "/api/admin/prompts", body, nil), 201) // Safe retry of the same creation.
	changed := map[string]string{"id": id, "title": "不同内容", "content": "不同内容"}
	assertStatus(t, authRequest(h, admin, "POST", "/api/admin/prompts", changed, nil), 409)
	path := "/api/admin/prompts/" + id + "/images/"
	assertStatus(t, binaryLibraryRequest(h, user, path+firstImage, []byte("first")), 403)
	assertStatus(t, binaryLibraryRequest(h, admin, path+firstImage, []byte("first")), 201)
	assertStatus(t, binaryLibraryRequest(h, admin, path+firstImage, []byte("first")), 200) // Same image ID does not upload twice.
	assertStatus(t, binaryLibraryRequest(h, admin, path+secondImage, []byte("second")), 201)
	w := authRequest(h, nil, "GET", "/api/prompts?q=%E9%9B%A8%E5%A4%9C", nil, nil)
	assertStatus(t, w, 200)
	var list struct {
		Prompts []persistence.LibraryPrompt `json:"prompts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Prompts) != 1 || len(list.Prompts[0].Images) != 2 {
		t.Fatalf("public list: %s %v", w.Body, err)
	}
	if strings.Contains(w.Body.String(), "object_key") || strings.Contains(w.Body.String(), "prompt-library/") {
		t.Fatal("public JSON leaked a storage key")
	}
	w = authRequest(h, nil, "GET", "/api/prompt-images/"+firstImage, nil, nil)
	assertStatus(t, w, 200)
	if w.Body.String() != "first" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("public image mismatch")
	}
	assertStatus(t, authRequest(h, nil, "GET", "/api/assets/content?key=private", nil, nil), 401)
	assertStatus(t, authRequest(h, user, "DELETE", "/api/admin/prompts/"+id, nil, nil), 403)
	media.failDelete = true
	assertStatus(t, authRequest(h, admin, "DELETE", path+firstImage, nil, nil), 204)
	assertStatus(t, authRequest(h, nil, "GET", "/api/prompt-images/"+firstImage, nil, nil), 404)
	var queued int
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM prompt_image_cleanup`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("cleanup must be retained: %d %v", queued, err)
	}
	media.failDelete = false
	assertStatus(t, authRequest(h, admin, "GET", "/api/admin/prompts", nil, nil), 200)
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM prompt_image_cleanup`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("cleanup retry failed: %d %v", queued, err)
	}
	assertStatus(t, authRequest(h, admin, "DELETE", "/api/admin/prompts/"+id, nil, nil), 204)
	assertStatus(t, authRequest(h, nil, "GET", "/api/prompt-images/"+secondImage, nil, nil), 404)
	if len(media.objects) != 0 {
		t.Fatalf("orphaned test objects: %d", len(media.objects))
	}
}
