package displayclient

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/je4/utils/v2/pkg/zLogger"
	rawclient "github.com/je4/wsbrokerclient/pkg/client"
	"github.com/je4/wsbrokerclient/pkg/event"
	"github.com/je4/wsbrokerdisplay/pkg/rest"
	"github.com/rs/zerolog"
)

func newTestLogger() zLogger.ZLogger {
	nop := zerolog.Nop()
	var logger zLogger.ZLogger = &nop
	return logger
}

type mockBrokerClient struct {
	name          string
	mu            sync.Mutex
	handlers      []rawclient.EventHandler
	sentEvents    []*event.Event
	onSend        func(ctx context.Context, evt *event.Event) error
	connectCalled bool
	closed        bool
}

func newMockBrokerClient(name string) *mockBrokerClient {
	return &mockBrokerClient{name: name}
}

func (m *mockBrokerClient) ClientName() string {
	return m.name
}

func (m *mockBrokerClient) Send(ctx context.Context, evt *event.Event) error {
	m.mu.Lock()
	m.sentEvents = append(m.sentEvents, evt)
	onSend := m.onSend
	m.mu.Unlock()

	if onSend != nil {
		return onSend(ctx, evt)
	}
	return nil
}

func (m *mockBrokerClient) OnEvent(handler rawclient.EventHandler) rawclient.UnsubscribeFunc {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, handler)
	idx := len(m.handlers) - 1
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if idx < len(m.handlers) {
			m.handlers[idx] = nil
		}
	}
}

func (m *mockBrokerClient) OnMessage(handler rawclient.EventHandler) rawclient.UnsubscribeFunc {
	return m.OnEvent(handler)
}

func (m *mockBrokerClient) OnDirect(handler rawclient.EventHandler) rawclient.UnsubscribeFunc {
	return m.OnEvent(handler)
}

func (m *mockBrokerClient) OnChannel(channel string, handler rawclient.EventHandler) rawclient.UnsubscribeFunc {
	return m.OnEvent(handler)
}

func (m *mockBrokerClient) Connect(ctx context.Context) error {
	m.mu.Lock()
	m.connectCalled = true
	m.mu.Unlock()
	return nil
}

func (m *mockBrokerClient) ConnectSync(ctx context.Context) error {
	m.mu.Lock()
	m.connectCalled = true
	m.mu.Unlock()
	return nil
}

func (m *mockBrokerClient) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return nil
}

func (m *mockBrokerClient) EmitEvent(evt *event.Event) {
	m.mu.Lock()
	handlers := make([]rawclient.EventHandler, len(m.handlers))
	copy(handlers, m.handlers)
	m.mu.Unlock()

	for _, h := range handlers {
		if h != nil {
			h(evt)
		}
	}
}

func TestClientNew(t *testing.T) {
	logger := newTestLogger()

	// 1. Verify New with nil raw client returns error
	c, err := New(nil, logger)
	if err == nil {
		t.Fatalf("expected error when raw client is nil, got nil")
	}
	if c != nil {
		t.Fatalf("expected nil client instance on error, got %+v", c)
	}

	// 2. Verify New with mock client and options
	mock := newMockBrokerClient("controller01")
	c, err = New(mock, logger,
		WithTimeout(5*time.Second),
		WithDefaultScreenshotOptions(ScreenshotOptions{Format: "png", Quality: 95}),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if c.ClientName() != "controller01" {
		t.Errorf("expected client name controller01, got %s", c.ClientName())
	}
	if c.RawClient() != mock {
		t.Errorf("expected RawClient() to match injected mock client")
	}
	if c.defaultTimeout != 5*time.Second {
		t.Errorf("expected defaultTimeout 5s, got %v", c.defaultTimeout)
	}
	if c.defaultScreenshotOpts.Format != "png" || c.defaultScreenshotOpts.Quality != 95 {
		t.Errorf("unexpected screenshot options: %+v", c.defaultScreenshotOpts)
	}
}

func TestSerialization(t *testing.T) {
	// CommandMessage
	cmd := CommandMessage{
		Event:    EventNavigate,
		Action:   ActionNavigate,
		URL:      "https://example.com/test",
		Selector: "#main-content",
		Format:   "png",
		Quality:  90,
	}
	cmdData, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("failed to marshal CommandMessage: %v", err)
	}
	var parsedCmd CommandMessage
	if err := json.Unmarshal(cmdData, &parsedCmd); err != nil {
		t.Fatalf("failed to unmarshal CommandMessage: %v", err)
	}
	if parsedCmd.Event != EventNavigate || parsedCmd.Action != ActionNavigate || parsedCmd.URL != "https://example.com/test" || parsedCmd.Selector != "#main-content" {
		t.Errorf("unexpected parsed CommandMessage: %+v", parsedCmd)
	}

	// NavigateResponse
	navResp := NavigateResponse{
		Event:  EventNavigateResponse,
		Action: ActionNavigateResponse,
		Status: "ok",
		URL:    "https://example.com/page",
	}
	navData, err := json.Marshal(navResp)
	if err != nil {
		t.Fatalf("failed to marshal NavigateResponse: %v", err)
	}
	var parsedNav NavigateResponse
	if err := json.Unmarshal(navData, &parsedNav); err != nil {
		t.Fatalf("failed to unmarshal NavigateResponse: %v", err)
	}
	if parsedNav.Event != EventNavigateResponse || parsedNav.Status != "ok" || parsedNav.URL != "https://example.com/page" {
		t.Errorf("unexpected NavigateResponse: %+v", parsedNav)
	}

	// URLResponse
	urlResp := URLResponse{
		Event: EventGetURLResponse,
		URL:   "https://example.com/active",
	}
	urlData, err := json.Marshal(urlResp)
	if err != nil {
		t.Fatalf("failed to marshal URLResponse: %v", err)
	}
	var parsedURL URLResponse
	if err := json.Unmarshal(urlData, &parsedURL); err != nil {
		t.Fatalf("failed to unmarshal URLResponse: %v", err)
	}
	if parsedURL.Event != EventGetURLResponse || parsedURL.URL != "https://example.com/active" {
		t.Errorf("unexpected URLResponse: %+v", parsedURL)
	}

	// ScreenshotResponse
	shotResp := ScreenshotResponse{
		Event:  EventScreenshotResponse,
		Status: "error",
		Error:  "context deadline exceeded",
	}
	shotData, err := json.Marshal(shotResp)
	if err != nil {
		t.Fatalf("failed to marshal ScreenshotResponse: %v", err)
	}
	var parsedShot ScreenshotResponse
	if err := json.Unmarshal(shotData, &parsedShot); err != nil {
		t.Fatalf("failed to unmarshal ScreenshotResponse: %v", err)
	}
	if parsedShot.Status != "error" || parsedShot.Error != "context deadline exceeded" {
		t.Errorf("unexpected ScreenshotResponse: %+v", parsedShot)
	}

	// StatusResponse
	statResp := StatusResponse{
		Event:  EventGetStatusResponse,
		Status: "ok",
		Data: &rest.DashboardData{
			ClientName: "display01",
			Hostname:   "test-host",
		},
	}
	statData, err := json.Marshal(statResp)
	if err != nil {
		t.Fatalf("failed to marshal StatusResponse: %v", err)
	}
	var parsedStat StatusResponse
	if err := json.Unmarshal(statData, &parsedStat); err != nil {
		t.Fatalf("failed to unmarshal StatusResponse: %v", err)
	}
	if parsedStat.Data == nil || parsedStat.Data.ClientName != "display01" {
		t.Errorf("unexpected StatusResponse: %+v", parsedStat)
	}
}

func TestNavigateSync(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	// Set onSend handler to simulate target display reply
	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		var cmd CommandMessage
		if err := evt.UnmarshalData(&cmd); err != nil {
			t.Errorf("failed to unmarshal cmd: %v", err)
		}
		if cmd.URL != "https://example.com" || cmd.Selector != "#root" {
			t.Errorf("unexpected command payload: %+v", cmd)
		}

		resp := NavigateResponse{
			Event:  EventNavigateResponse,
			Status: "ok",
			URL:    cmd.URL,
		}
		respEvt, err := event.NewMessageEvent("disp01", "ctrl01", resp)
		if err != nil {
			return err
		}
		respEvt.Token = evt.Token
		go mock.EmitEvent(respEvt)
		return nil
	}

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.Navigate(ctx, "disp01", "https://example.com", "#root")
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	if resp == nil || resp.Status != "ok" || resp.URL != "https://example.com" {
		t.Fatalf("unexpected NavigateResponse: %+v", resp)
	}
}

func TestNavigateSyncError(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		resp := NavigateResponse{
			Event:  EventNavigateResponse,
			Status: "error",
			Error:  "network timeout resolving host",
		}
		respEvt, err := event.NewMessageEvent("disp01", "ctrl01", resp)
		if err != nil {
			return err
		}
		respEvt.Token = evt.Token
		go mock.EmitEvent(respEvt)
		return nil
	}

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.Navigate(ctx, "disp01", "https://invalid.domain")
	if err == nil {
		t.Fatalf("expected error from failed Navigate, got nil")
	}
	if resp == nil || resp.Status != "error" {
		t.Fatalf("expected error NavigateResponse, got %+v", resp)
	}
}

func TestGetURLSync(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		resp := URLResponse{
			Event: EventGetURLResponse,
			URL:   "https://example.com/current-dashboard",
		}
		respEvt, err := event.NewMessageEvent("disp01", "ctrl01", resp)
		if err != nil {
			return err
		}
		respEvt.Token = evt.Token
		go mock.EmitEvent(respEvt)
		return nil
	}

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	activeURL, err := client.GetURL(ctx, "disp01")
	if err != nil {
		t.Fatalf("GetURL failed: %v", err)
	}
	if activeURL != "https://example.com/current-dashboard" {
		t.Fatalf("unexpected active URL: %s", activeURL)
	}
}

func TestGetURLSyncError(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		resp := URLResponse{
			Event: EventGetURLResponse,
			Error: "browser page context unavailable",
		}
		respEvt, err := event.NewMessageEvent("disp01", "ctrl01", resp)
		if err != nil {
			return err
		}
		respEvt.Token = evt.Token
		go mock.EmitEvent(respEvt)
		return nil
	}

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.GetURL(ctx, "disp01")
	if err == nil {
		t.Fatalf("expected error from GetURL, got nil")
	}
}

func TestGetScreenshotSyncFile(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	expectedBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}

	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		fileMsg := event.NewFileMessage("screen_800x600.jpg", "image/jpeg", expectedBytes)
		fileEvt, err := event.NewFileEvent("disp01", "ctrl01", fileMsg, evt.Token)
		if err != nil {
			return err
		}
		go mock.EmitEvent(fileEvt)
		return nil
	}

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := client.GetScreenshot(ctx, "disp01", ScreenshotOptions{Format: "jpeg", Quality: 80})
	if err != nil {
		t.Fatalf("GetScreenshot failed: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil ScreenshotResult")
	}
	if res.Filename != "screen_800x600.jpg" || res.MimeType != "image/jpeg" {
		t.Errorf("unexpected filename/mime: %s, %s", res.Filename, res.MimeType)
	}
	if len(res.Data) != len(expectedBytes) {
		t.Errorf("unexpected screenshot byte length: %d", len(res.Data))
	}
}

func TestGetScreenshotSyncFileLargePayload(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	// Create a payload larger than 32 KiB (e.g. 128 KiB = 131072 bytes)
	expectedBytes := make([]byte, 128*1024)
	for i := range expectedBytes {
		expectedBytes[i] = byte(i % 256)
	}

	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		fileMsg := event.NewFileMessage("large_screenshot_800x600.jpg", "image/jpeg", expectedBytes)
		fileEvt, err := event.NewFileEvent("disp01", "ctrl01", fileMsg, evt.Token)
		if err != nil {
			return err
		}
		go mock.EmitEvent(fileEvt)
		return nil
	}

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := client.GetScreenshot(ctx, "disp01", ScreenshotOptions{Format: "jpeg", Quality: 90})
	if err != nil {
		t.Fatalf("GetScreenshot failed for large payload: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil ScreenshotResult")
	}
	if res.Filename != "large_screenshot_800x600.jpg" || res.MimeType != "image/jpeg" {
		t.Errorf("unexpected filename/mime: %s, %s", res.Filename, res.MimeType)
	}
	if len(res.Data) != len(expectedBytes) {
		t.Errorf("unexpected screenshot byte length: got %d, expected %d", len(res.Data), len(expectedBytes))
	}
}

func TestGetScreenshotSyncErrorEnvelope(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		resp := ScreenshotResponse{
			Event:  EventScreenshotResponse,
			Status: "error",
			Error:  "rendering context detached",
		}
		respEvt, err := event.NewMessageEvent("disp01", "ctrl01", resp)
		if err != nil {
			return err
		}
		respEvt.Token = evt.Token
		go mock.EmitEvent(respEvt)
		return nil
	}

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.GetScreenshot(ctx, "disp01")
	if err == nil {
		t.Fatalf("expected error from failed GetScreenshot, got nil")
	}
}

func TestGetStatusSync(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		resp := StatusResponse{
			Event:  EventGetStatusResponse,
			Status: "ok",
			Data: &rest.DashboardData{
				ClientName: "disp01",
				WSBroker:   "wss://broker.example.com",
				OS:         "linux",
				Arch:       "amd64",
			},
		}
		respEvt, err := event.NewMessageEvent("disp01", "ctrl01", resp)
		if err != nil {
			return err
		}
		respEvt.Token = evt.Token
		go mock.EmitEvent(respEvt)
		return nil
	}

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := client.GetStatus(ctx, "disp01")
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status == nil || status.ClientName != "disp01" || status.WSBroker != "wss://broker.example.com" {
		t.Fatalf("unexpected DashboardData: %+v", status)
	}
}

func TestGetStatusSyncErrors(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	// 1. Error status envelope
	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		resp := StatusResponse{
			Event:  EventGetStatusResponse,
			Status: "error",
			Error:  "telemetry provider unavailable",
		}
		respEvt, err := event.NewMessageEvent("disp01", "ctrl01", resp)
		if err != nil {
			return err
		}
		respEvt.Token = evt.Token
		go mock.EmitEvent(respEvt)
		return nil
	}

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.GetStatus(ctx, "disp01")
	if err == nil {
		t.Fatalf("expected error from failed GetStatus, got nil")
	}

	// 2. Missing data payload
	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		resp := StatusResponse{
			Event:  EventGetStatusResponse,
			Status: "ok",
			Data:   nil,
		}
		respEvt, err := event.NewMessageEvent("disp01", "ctrl01", resp)
		if err != nil {
			return err
		}
		respEvt.Token = evt.Token
		go mock.EmitEvent(respEvt)
		return nil
	}

	_, err = client.GetStatus(ctx, "disp01")
	if err == nil {
		t.Fatalf("expected error when data is nil, got nil")
	}
}

func TestAsyncSendMethods(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()

	// 1. SendNavigate
	if err := client.SendNavigate(ctx, "disp01", "https://example.com/async", "#content", "custom-token-1"); err != nil {
		t.Fatalf("SendNavigate failed: %v", err)
	}

	// 2. SendGetURL
	if err := client.SendGetURL(ctx, "disp01", "custom-token-2"); err != nil {
		t.Fatalf("SendGetURL failed: %v", err)
	}

	// 3. SendGetScreenshot
	if err := client.SendGetScreenshot(ctx, "disp01", "png", 90, "custom-token-3"); err != nil {
		t.Fatalf("SendGetScreenshot failed: %v", err)
	}

	// 4. SendGetStatus
	if err := client.SendGetStatus(ctx, "disp01", "custom-token-4"); err != nil {
		t.Fatalf("SendGetStatus failed: %v", err)
	}

	mock.mu.Lock()
	events := mock.sentEvents
	mock.mu.Unlock()

	if len(events) != 4 {
		t.Fatalf("expected 4 sent events, got %d", len(events))
	}

	if events[0].Token != "custom-token-1" || events[0].Target != "disp01" {
		t.Errorf("unexpected SendNavigate event: %+v", events[0])
	}
	if events[1].Token != "custom-token-2" || events[1].Target != "disp01" {
		t.Errorf("unexpected SendGetURL event: %+v", events[1])
	}
	if events[2].Token != "custom-token-3" || events[2].Target != "disp01" {
		t.Errorf("unexpected SendGetScreenshot event: %+v", events[2])
	}
	if events[3].Token != "custom-token-4" || events[3].Target != "disp01" {
		t.Errorf("unexpected SendGetStatus event: %+v", events[3])
	}
}

func TestContextTimeoutAndCancellation(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	// Handler does not reply -> will time out
	mock.onSend = func(ctx context.Context, evt *event.Event) error {
		return nil
	}

	client, err := New(mock, logger, WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Test default timeout triggers
	_, err = client.Navigate(context.Background(), "disp01", "https://example.com")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}

	// Test explicit context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediate cancel

	_, err = client.GetURL(ctx, "disp01")
	if err == nil {
		t.Fatalf("expected context cancellation error, got nil")
	}
}

func TestClientClose(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Start a synchronous request in background
	errCh := make(chan error, 1)
	go func() {
		_, err := client.Navigate(context.Background(), "disp01", "https://example.com")
		errCh <- err
	}()

	time.Sleep(30 * time.Millisecond)

	// Close client while request is in flight
	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected error from in-flight request on client close, got nil")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("in-flight request did not terminate upon client close")
	}

	// Test idempotent Close
	if err := client.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}

	if !mock.closed {
		t.Errorf("expected underlying mock client to be closed")
	}
}

func TestUncorrelatedAndNilEvents(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Emitting nil or unrecognised events should not panic
	mock.EmitEvent(nil)
	mock.EmitEvent(&event.Event{Token: ""})
	mock.EmitEvent(&event.Event{Token: "non-existent-token"})

	_ = client
}

func TestClientDelegates(t *testing.T) {
	logger := newTestLogger()
	mock := newMockBrokerClient("ctrl01")

	client, err := New(mock, logger)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Errorf("Connect failed: %v", err)
	}
	if err := client.ConnectSync(ctx); err != nil {
		t.Errorf("ConnectSync failed: %v", err)
	}

	// Register event handlers
	unsub1 := client.OnEvent(func(evt *event.Event) {})
	unsub2 := client.OnMessage(func(evt *event.Event) {})
	unsub3 := client.OnDirect(func(evt *event.Event) {})
	unsub4 := client.OnChannel("ch1", func(evt *event.Event) {})

	unsub1()
	unsub2()
	unsub3()
	unsub4()
}
