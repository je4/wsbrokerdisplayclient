package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/je4/wsbrokerdisplay/pkg/rest"
	"github.com/je4/wsbrokerdisplayclient/pkg/displayclient"
	"github.com/je4/wsbrokerdisplayclient/pkg/manager"
)

type mockDisplayClient struct{}

func (m *mockDisplayClient) Navigate(ctx context.Context, target, url string, selector ...string) (*displayclient.NavigateResponse, error) {
	return &displayclient.NavigateResponse{
		Status: "ok",
		URL:    url,
	}, nil
}

func (m *mockDisplayClient) GetURL(ctx context.Context, target string) (string, error) {
	return "https://example.com/live", nil
}

func (m *mockDisplayClient) GetScreenshot(ctx context.Context, target string, opts ...displayclient.ScreenshotOptions) (*displayclient.ScreenshotResult, error) {
	return &displayclient.ScreenshotResult{
		Filename: "screen.jpg",
		MimeType: "image/jpeg",
		Data:     []byte("test-jpg-image-bytes"),
	}, nil
}

func (m *mockDisplayClient) GetStatus(ctx context.Context, target string) (*rest.DashboardData, error) {
	return &rest.DashboardData{
		Hostname: target,
		Uptime:   "1h 45m",
		BrowserConfig: rest.BrowserInfo{
			Address: "https://example.com/test",
			Width:   1920,
			Height:  1080,
			Kiosk:   true,
		},
	}, nil
}

func setupTestServer(t *testing.T) (*Server, *manager.Manager) {
	mockClient := &mockDisplayClient{}
	mgr := manager.NewManager(mockClient, []string{"display01", "display02"}, 10, nil)

	cfg := Config{
		ListenAddr: "localhost:8770",
		ExtAddr:    "http://localhost:8770",
		ClientName: "testclient",
		BrokerURL:  "https://localhost:8765",
	}

	server, err := NewServer(cfg, mgr, nil)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return server, mgr
}

func TestPing(t *testing.T) {
	server, _ := setupTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/ping", nil)
	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "pong" {
		t.Fatalf("expected 'pong', got %q", w.Body.String())
	}
}

func TestDashboard(t *testing.T) {
	server, _ := setupTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "WSBroker Display Controller") {
		t.Errorf("expected dashboard title in HTML")
	}
	if !strings.Contains(body, "testclient") {
		t.Errorf("expected client name 'testclient' in HTML")
	}
}

func TestDisplaysAndActions(t *testing.T) {
	server, _ := setupTestServer(t)

	// 1. GET /displays
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/displays", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /displays failed with %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "display01") {
		t.Errorf("expected display01 in cards response")
	}

	// 2. POST /api/targets/add
	w = httptest.NewRecorder()
	form := url.Values{}
	form.Set("id", "display03")
	form.Set("name", "New Lobby Screen")
	req, _ = http.NewRequest(http.MethodPost, "/api/targets/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/targets/add failed with %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "New Lobby Screen") {
		t.Errorf("expected New Lobby Screen in cards after add")
	}

	// 3. POST /api/displays/display01/geturl
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/displays/display01/geturl", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/displays/display01/geturl failed with %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "https://example.com/live") {
		t.Errorf("expected URL in card response")
	}

	// 4. POST /api/displays/display01/status
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/displays/display01/status", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/displays/display01/status failed with %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "1920x1080") {
		t.Errorf("expected resolution 1920x1080 in card response")
	}

	// 5. POST /api/displays/display01/screenshot
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/displays/display01/screenshot", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/displays/display01/screenshot failed with %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "/screenshots/") {
		t.Errorf("expected screenshot link in card response, got: %s", w.Body.String())
	}
}

func TestModalsAndHistory(t *testing.T) {
	server, mgr := setupTestServer(t)

	// 1. GET /modal/navigate
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/modal/navigate?target=display01", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /modal/navigate failed: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "display01") {
		t.Errorf("expected prefilled target in modal")
	}

	// 2. POST /api/displays/navigate
	w = httptest.NewRecorder()
	form := url.Values{}
	form.Set("target", "display01")
	form.Set("url", "https://golang.org")
	req, _ = http.NewRequest(http.MethodPost, "/api/displays/navigate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/displays/navigate failed: %d", w.Code)
	}

	// 3. GET /history
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/history", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /history failed: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "golang.org") {
		t.Errorf("expected command in history table")
	}

	// 4. POST /history/clear
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/history/clear", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /history/clear failed: %d", w.Code)
	}

	// 5. Screenshot Raw Serving
	item := mgr.StoreScreenshot("display01", "shot.png", "image/png", []byte("raw-png-bytes"))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/screenshots/"+item.ID+"/raw", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /screenshots/:id/raw failed: %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("expected content type image/png, got %s", w.Header().Get("Content-Type"))
	}
	if w.Body.String() != "raw-png-bytes" {
		t.Errorf("unexpected image body: %s", w.Body.String())
	}
}

func TestBulkOperationsAndTargetDeletion(t *testing.T) {
	server, mgr := setupTestServer(t)

	// 1. Bulk Navigate
	form := url.Values{}
	form.Set("url", "https://example.com/bulk")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/displays/bulk/navigate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/displays/bulk/navigate failed: %d", w.Code)
	}

	// 2. Bulk Status
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/displays/bulk/status", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/displays/bulk/status failed: %d", w.Code)
	}

	// 3. Bulk Screenshot
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/displays/bulk/screenshot", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/displays/bulk/screenshot failed: %d", w.Code)
	}

	// 4. Stats
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/stats", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/stats failed: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "total_displays") {
		t.Errorf("expected stats JSON payload, got: %s", w.Body.String())
	}

	// 5. Target Deletion (DELETE)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/api/targets/display01", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /api/targets/:id failed: %d", w.Code)
	}

	if _, exists := mgr.GetTarget("display01"); exists {
		t.Errorf("expected display01 to be removed from manager")
	}

	// 6. Target Deletion (POST delete)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/targets/display02/delete", nil)
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/targets/:id/delete failed: %d", w.Code)
	}

	if len(mgr.GetDisplays()) != 0 {
		t.Errorf("expected 0 displays, got %d", len(mgr.GetDisplays()))
	}
}

func TestSSEEvents(t *testing.T) {
	server, mgr := setupTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/events", nil)

	go func() {
		// Broadcast event after short delay
		mgr.BroadcastToast("info", "SSE Test", "Streaming notification")
		cancel() // Close SSE stream context
	}()

	server.Router().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "event: stats_updated") {
		t.Errorf("expected initial stats_updated event in SSE stream, got: %s", body)
	}
}
