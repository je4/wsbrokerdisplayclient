package manager

import (
	"context"
	"fmt"
	"testing"

	"github.com/je4/wsbrokerdisplay/pkg/rest"
	"github.com/je4/wsbrokerdisplayclient/pkg/displayclient"
)

type mockDisplayController struct {
	navigateFunc   func(ctx context.Context, target, url string, selector ...string) (*displayclient.NavigateResponse, error)
	getURLFunc     func(ctx context.Context, target string) (string, error)
	screenshotFunc func(ctx context.Context, target string, opts ...displayclient.ScreenshotOptions) (*displayclient.ScreenshotResult, error)
	statusFunc     func(ctx context.Context, target string) (*rest.DashboardData, error)
}

func (m *mockDisplayController) Navigate(ctx context.Context, target, url string, selector ...string) (*displayclient.NavigateResponse, error) {
	if m.navigateFunc != nil {
		return m.navigateFunc(ctx, target, url, selector...)
	}
	return &displayclient.NavigateResponse{
		Status: "ok",
		URL:    url,
	}, nil
}

func (m *mockDisplayController) GetURL(ctx context.Context, target string) (string, error) {
	if m.getURLFunc != nil {
		return m.getURLFunc(ctx, target)
	}
	return "https://example.com/live", nil
}

func (m *mockDisplayController) GetScreenshot(ctx context.Context, target string, opts ...displayclient.ScreenshotOptions) (*displayclient.ScreenshotResult, error) {
	if m.screenshotFunc != nil {
		return m.screenshotFunc(ctx, target, opts...)
	}
	return &displayclient.ScreenshotResult{
		Filename: "shot.jpg",
		MimeType: "image/jpeg",
		Data:     []byte("test-image-binary-data"),
	}, nil
}

func (m *mockDisplayController) GetStatus(ctx context.Context, target string) (*rest.DashboardData, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx, target)
	}
	return &rest.DashboardData{
		Hostname: target,
		Uptime:   "2h 15m",
		BrowserConfig: rest.BrowserInfo{
			Address: "https://example.com",
			Width:   1920,
			Height:  1080,
			Kiosk:   true,
		},
	}, nil
}

func TestManagerTargets(t *testing.T) {
	mgr := NewManager(nil, []string{"display01", "display02"}, 10, nil)

	displays := mgr.GetDisplays()
	if len(displays) != 2 {
		t.Fatalf("expected 2 default displays, got %d", len(displays))
	}
	if displays[0].ID != "display01" || displays[1].ID != "display02" {
		t.Errorf("unexpected display IDs: %v", displays)
	}

	mgr.AddTarget("display03", "Lobby Screen", "lobby")
	target, exists := mgr.GetTarget("display03")
	if !exists {
		t.Fatalf("expected display03 to exist")
	}
	if target.Name != "Lobby Screen" || target.Channel != "lobby" {
		t.Errorf("unexpected target fields: %+v", target)
	}

	displays = mgr.GetDisplays()
	if len(displays) != 3 {
		t.Fatalf("expected 3 displays, got %d", len(displays))
	}
}

func TestManagerScreenshotCache(t *testing.T) {
	mgr := NewManager(nil, []string{"disp1"}, 10, nil)
	mgr.maxScreenshots = 3

	for i := 1; i <= 5; i++ {
		mgr.StoreScreenshot("disp1", fmt.Sprintf("img%d.jpg", i), "image/jpeg", []byte(fmt.Sprintf("data%d", i)))
	}

	if len(mgr.screenshots) != 3 {
		t.Errorf("expected max 3 screenshots in cache, got %d", len(mgr.screenshots))
	}

	target, _ := mgr.GetTarget("disp1")
	if target.LatestImage == nil || target.LatestImage.Filename != "img5.jpg" {
		t.Errorf("expected latest image to be img5.jpg, got %+v", target.LatestImage)
	}
}

func TestManagerOperations(t *testing.T) {
	mock := &mockDisplayController{}
	mgr := NewManager(mock, []string{"display01"}, 5, nil)
	ctx := context.Background()

	// 1. Navigate
	navResp, err := mgr.Navigate(ctx, "display01", "https://golang.org", "body")
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	if navResp.URL != "https://golang.org" {
		t.Errorf("unexpected active URL: %s", navResp.URL)
	}

	// 2. GetURL
	url, err := mgr.GetURL(ctx, "display01")
	if err != nil {
		t.Fatalf("GetURL failed: %v", err)
	}
	if url != "https://example.com/live" {
		t.Errorf("unexpected URL: %s", url)
	}

	// 3. GetStatus
	status, err := mgr.GetStatus(ctx, "display01")
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.BrowserConfig.Width != 1920 {
		t.Errorf("unexpected status width: %d", status.BrowserConfig.Width)
	}

	// 4. CaptureScreenshot
	item, err := mgr.CaptureScreenshot(ctx, "display01")
	if err != nil {
		t.Fatalf("CaptureScreenshot failed: %v", err)
	}
	if string(item.Data) != "test-image-binary-data" {
		t.Errorf("unexpected image data: %s", string(item.Data))
	}

	// Check History Log
	history := mgr.GetHistory()
	if len(history) != 4 {
		t.Fatalf("expected 4 history entries, got %d", len(history))
	}
	if history[0].Status != "OK" {
		t.Errorf("expected OK status in history, got %s", history[0].Status)
	}

	mgr.ClearHistory()
	if len(mgr.GetHistory()) != 0 {
		t.Errorf("expected empty history after clear")
	}
}

func TestManagerEventBroadcasterAndStats(t *testing.T) {
	mock := &mockDisplayController{}
	mgr := NewManager(mock, []string{"display01"}, 10, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventCh := mgr.Subscribe(ctx)

	// Add target
	mgr.AddTarget("display02", "Display 2", "ch1")

	select {
	case ev := <-eventCh:
		if ev.Type != EventTargetAdded {
			t.Errorf("expected EventTargetAdded, got %s", ev.Type)
		}
	default:
		t.Errorf("expected to receive EventTargetAdded event")
	}

	// Drain stats event from add target
	select {
	case ev := <-eventCh:
		if ev.Type != EventStatsUpdated {
			t.Errorf("expected EventStatsUpdated, got %s", ev.Type)
		}
	default:
	}

	// Remove target
	removed := mgr.RemoveTarget("display02")
	if !removed {
		t.Fatalf("expected display02 to be removed")
	}

	select {
	case ev := <-eventCh:
		if ev.Type != EventTargetRemoved {
			t.Errorf("expected EventTargetRemoved, got %s", ev.Type)
		}
	default:
		t.Errorf("expected to receive EventTargetRemoved event")
	}

	// Drain stats event
	select {
	case <-eventCh:
	default:
	}

	stats := mgr.GetStats()
	if stats.TotalDisplays != 1 {
		t.Errorf("expected 1 total display, got %d", stats.TotalDisplays)
	}

	// Broadcast Toast
	mgr.BroadcastToast("success", "Test Toast", "Test Message", "display01")
	select {
	case ev := <-eventCh:
		if ev.Type != EventToast {
			t.Errorf("expected EventToast, got %s", ev.Type)
		}
		toast, ok := ev.Data.(ToastMessage)
		if !ok || toast.Title != "Test Toast" {
			t.Errorf("unexpected toast payload: %+v", ev.Data)
		}
	default:
		t.Errorf("expected to receive EventToast event")
	}
}

func TestManagerBulkOperations(t *testing.T) {
	mock := &mockDisplayController{}
	mgr := NewManager(mock, []string{"display01", "display02"}, 10, nil)
	ctx := context.Background()

	// 1. NavigateAll
	errs := mgr.NavigateAll(ctx, "https://example.com/all", "body")
	if len(errs) != 0 {
		t.Errorf("NavigateAll returned errors: %v", errs)
	}

	displays := mgr.GetDisplays()
	for _, d := range displays {
		if d.CurrentURL != "https://example.com/all" {
			t.Errorf("expected target %s to have current url https://example.com/all, got %s", d.ID, d.CurrentURL)
		}
	}

	// 2. RefreshAll
	errs = mgr.RefreshAll(ctx)
	if len(errs) != 0 {
		t.Errorf("RefreshAll returned errors: %v", errs)
	}

	// 3. CaptureAll
	errs = mgr.CaptureAll(ctx)
	if len(errs) != 0 {
		t.Errorf("CaptureAll returned errors: %v", errs)
	}

	stats := mgr.GetStats()
	if stats.TotalCommands < 6 {
		t.Errorf("expected at least 6 commands recorded, got %d", stats.TotalCommands)
	}
}
