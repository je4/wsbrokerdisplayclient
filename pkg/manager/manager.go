package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/je4/utils/v2/pkg/zLogger"
	"github.com/je4/wsbrokerdisplay/pkg/rest"
	"github.com/je4/wsbrokerdisplayclient/pkg/displayclient"
)

// DisplayController defines the interface for issuing remote display operations.
type DisplayController interface {
	Navigate(ctx context.Context, target, url string, selector ...string) (*displayclient.NavigateResponse, error)
	GetURL(ctx context.Context, target string) (string, error)
	GetScreenshot(ctx context.Context, target string, opts ...displayclient.ScreenshotOptions) (*displayclient.ScreenshotResult, error)
	GetStatus(ctx context.Context, target string) (*rest.DashboardData, error)
}

// EventType defines the type of SSE/manager events.
type EventType string

const (
	EventDisplayUpdated EventType = "display_updated"
	EventCommandLogged  EventType = "command_logged"
	EventTargetAdded    EventType = "target_added"
	EventTargetRemoved  EventType = "target_removed"
	EventToast          EventType = "toast"
	EventStatsUpdated   EventType = "stats_updated"
)

// Event represents a real-time event sent across the manager event bus.
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Target    string      `json:"target,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

// DashboardStats holds summary metrics for display fleet monitoring.
type DashboardStats struct {
	TotalDisplays   int `json:"total_displays"`
	ActiveDisplays  int `json:"active_displays"`
	OfflineDisplays int `json:"offline_displays"`
	TotalCommands   int `json:"total_commands"`
}

// ToastMessage represents a user notification payload.
type ToastMessage struct {
	Level   string `json:"level"` // "success", "error", "info"
	Title   string `json:"title"`
	Message string `json:"message"`
}

// DisplayTarget represents a managed display node or channel.
type DisplayTarget struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Channel     string              `json:"channel,omitempty"`
	LastSeen    time.Time           `json:"last_seen"`
	CurrentURL  string              `json:"current_url"`
	LastStatus  *rest.DashboardData `json:"last_status,omitempty"`
	LatestImage *ScreenshotItem     `json:"latest_image,omitempty"`
}

// IsActive returns true if the target has been updated recently (within 2 minutes).
func (dt *DisplayTarget) IsActive() bool {
	if dt.LastSeen.IsZero() {
		return false
	}
	return time.Since(dt.LastSeen) < 2*time.Minute
}

// ScreenshotItem stores captured screenshot metadata and binary data.
type ScreenshotItem struct {
	ID        string    `json:"id"`
	Target    string    `json:"target"`
	Filename  string    `json:"filename"`
	MimeType  string    `json:"mime_type"`
	Timestamp time.Time `json:"timestamp"`
	Data      []byte    `json:"-"`
}

// CommandLogEntry records an executed command for audit and UI history.
type CommandLogEntry struct {
	ID        string        `json:"id"`
	Timestamp time.Time     `json:"timestamp"`
	Target    string        `json:"target"`
	Command   string        `json:"command"`
	Duration  time.Duration `json:"duration"`
	Status    string        `json:"status"` // "OK" or "ERROR"
	Message   string        `json:"message,omitempty"`
}

// Manager maintains thread-safe display states, screenshot caches, and command execution logs.
type Manager struct {
	client          DisplayController
	mu              sync.RWMutex
	targets         map[string]*DisplayTarget
	targetOrder     []string
	screenshots     map[string]*ScreenshotItem
	screenshotOrder []string
	maxScreenshots  int
	history         []CommandLogEntry
	maxHistory      int
	totalCommands   int
	subMu           sync.RWMutex
	subscribers     map[chan Event]struct{}
	logger          zLogger.ZLogger
}

// NewManager creates a new Manager instance.
func NewManager(client DisplayController, defaultTargets []string, maxHistory int, logger zLogger.ZLogger) *Manager {
	if maxHistory <= 0 {
		maxHistory = 100
	}
	m := &Manager{
		client:          client,
		targets:         make(map[string]*DisplayTarget),
		targetOrder:     make([]string, 0),
		screenshots:     make(map[string]*ScreenshotItem),
		screenshotOrder: make([]string, 0),
		maxScreenshots:  50,
		history:         make([]CommandLogEntry, 0, maxHistory),
		maxHistory:      maxHistory,
		subscribers:     make(map[chan Event]struct{}),
		logger:          logger,
	}

	for _, target := range defaultTargets {
		if target != "" {
			m.AddTarget(target, target, "")
		}
	}

	return m
}

// Subscribe returns a read channel for receiving real-time events. The subscription is canceled when ctx is done.
func (m *Manager) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, 64)
	m.subMu.Lock()
	m.subscribers[ch] = struct{}{}
	m.subMu.Unlock()

	go func() {
		<-ctx.Done()
		m.subMu.Lock()
		delete(m.subscribers, ch)
		close(ch)
		m.subMu.Unlock()
	}()

	return ch
}

// Broadcast sends an event to all active subscribers without blocking.
func (m *Manager) Broadcast(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	m.subMu.RLock()
	defer m.subMu.RUnlock()

	for ch := range m.subscribers {
		select {
		case ch <- event:
		default:
			// Dropped if subscriber buffer is full to prevent lagging clients from stalling publisher
		}
	}
}

// BroadcastToast broadcasts a user notification toast across SSE.
func (m *Manager) BroadcastToast(level, title, message string, target ...string) {
	tgt := ""
	if len(target) > 0 {
		tgt = target[0]
	}
	m.Broadcast(Event{
		Type:      EventToast,
		Timestamp: time.Now(),
		Target:    tgt,
		Data: ToastMessage{
			Level:   level,
			Title:   title,
			Message: message,
		},
	})
}

// GetStats returns summary telemetry and metrics for the dashboard.
func (m *Manager) GetStats() DashboardStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.targetOrder)
	active := 0
	offline := 0
	for _, target := range m.targets {
		if target.IsActive() {
			active++
		} else {
			offline++
		}
	}

	return DashboardStats{
		TotalDisplays:   total,
		ActiveDisplays:  active,
		OfflineDisplays: offline,
		TotalCommands:   m.totalCommands,
	}
}

func (m *Manager) broadcastStats() {
	stats := m.GetStats()
	m.Broadcast(Event{
		Type:      EventStatsUpdated,
		Timestamp: time.Now(),
		Data:      stats,
	})
}

// AddTarget registers or updates a target display.
func (m *Manager) AddTarget(id, name, channel string) *DisplayTarget {
	if id == "" {
		return nil
	}
	if name == "" {
		name = id
	}

	m.mu.Lock()
	target, exists := m.targets[id]
	if !exists {
		target = &DisplayTarget{
			ID:      id,
			Name:    name,
			Channel: channel,
		}
		m.targets[id] = target
		m.targetOrder = append(m.targetOrder, id)
	} else {
		target.Name = name
		if channel != "" {
			target.Channel = channel
		}
	}
	copyTarget := *target
	m.mu.Unlock()

	m.Broadcast(Event{
		Type:      EventTargetAdded,
		Timestamp: time.Now(),
		Target:    id,
		Data:      &copyTarget,
	})
	m.broadcastStats()

	return target
}

// RemoveTarget deletes a target display and notifies subscribers.
func (m *Manager) RemoveTarget(id string) bool {
	m.mu.Lock()
	_, exists := m.targets[id]
	if !exists {
		m.mu.Unlock()
		return false
	}
	delete(m.targets, id)
	newOrder := make([]string, 0, len(m.targetOrder)-1)
	for _, tid := range m.targetOrder {
		if tid != id {
			newOrder = append(newOrder, tid)
		}
	}
	m.targetOrder = newOrder
	m.mu.Unlock()

	m.Broadcast(Event{
		Type:      EventTargetRemoved,
		Timestamp: time.Now(),
		Target:    id,
		Data:      id,
	})
	m.broadcastStats()
	return true
}

// GetTarget returns a copy of a display target by ID.
func (m *Manager) GetTarget(id string) (*DisplayTarget, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	target, exists := m.targets[id]
	if !exists {
		return nil, false
	}
	copyTarget := *target
	return &copyTarget, true
}

// GetDisplays returns an ordered slice of all known display targets.
func (m *Manager) GetDisplays() []*DisplayTarget {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*DisplayTarget, 0, len(m.targetOrder))
	for _, id := range m.targetOrder {
		if target, exists := m.targets[id]; exists {
			copyTarget := *target
			result = append(result, &copyTarget)
		}
	}
	return result
}

// GetHistory returns a copy of the command execution history (newest first).
func (m *Manager) GetHistory() []CommandLogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]CommandLogEntry, len(m.history))
	copy(result, m.history)
	return result
}

// ClearHistory resets the command execution history log.
func (m *Manager) ClearHistory() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = make([]CommandLogEntry, 0, m.maxHistory)
}

func (m *Manager) recordLog(target, command string, duration time.Duration, status, message string) {
	m.mu.Lock()
	entry := CommandLogEntry{
		ID:        generateID("cmd"),
		Timestamp: time.Now(),
		Target:    target,
		Command:   command,
		Duration:  duration,
		Status:    status,
		Message:   message,
	}

	m.totalCommands++
	// Prepend entry (newest first)
	m.history = append([]CommandLogEntry{entry}, m.history...)
	if len(m.history) > m.maxHistory {
		m.history = m.history[:m.maxHistory]
	}
	m.mu.Unlock()

	m.Broadcast(Event{
		Type:      EventCommandLogged,
		Timestamp: entry.Timestamp,
		Target:    target,
		Data:      entry,
	})
	m.broadcastStats()
}

// StoreScreenshot saves a screenshot to the in-memory cache and updates target's latest screenshot.
func (m *Manager) StoreScreenshot(target, filename, mimeType string, data []byte) *ScreenshotItem {
	m.mu.Lock()

	item := &ScreenshotItem{
		ID:        generateID("sc"),
		Target:    target,
		Filename:  filename,
		MimeType:  mimeType,
		Timestamp: time.Now(),
		Data:      data,
	}

	m.screenshots[item.ID] = item
	m.screenshotOrder = append(m.screenshotOrder, item.ID)

	// Enforce FIFO cache bounds
	for len(m.screenshotOrder) > m.maxScreenshots {
		oldID := m.screenshotOrder[0]
		m.screenshotOrder = m.screenshotOrder[1:]
		delete(m.screenshots, oldID)
	}

	var updatedTarget *DisplayTarget
	// Update target's latest image reference
	if t, exists := m.targets[target]; exists {
		t.LatestImage = item
		t.LastSeen = time.Now()
		cp := *t
		updatedTarget = &cp
	}
	m.mu.Unlock()

	if updatedTarget != nil {
		m.Broadcast(Event{
			Type:      EventDisplayUpdated,
			Timestamp: time.Now(),
			Target:    target,
			Data:      updatedTarget,
		})
		m.broadcastStats()
	}

	return item
}

// GetScreenshot returns a cached screenshot by ID.
func (m *Manager) GetScreenshot(id string) (*ScreenshotItem, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	item, exists := m.screenshots[id]
	if !exists {
		return nil, false
	}
	return item, true
}

// Navigate dispatches a navigation command to the target display.
func (m *Manager) Navigate(ctx context.Context, target, url string, selector ...string) (*displayclient.NavigateResponse, error) {
	if m.client == nil {
		return nil, errors.New("display client not initialized")
	}

	start := time.Now()
	cmdStr := fmt.Sprintf("navigate -> %s", url)
	if len(selector) > 0 && selector[0] != "" {
		cmdStr += fmt.Sprintf(" (wait: %s)", selector[0])
	}

	resp, err := m.client.Navigate(ctx, target, url, selector...)
	duration := time.Since(start)

	if err != nil {
		m.recordLog(target, cmdStr, duration, "ERROR", err.Error())
		return nil, err
	}

	var updatedTarget *DisplayTarget
	m.mu.Lock()
	if t, exists := m.targets[target]; exists {
		t.CurrentURL = resp.URL
		t.LastSeen = time.Now()
		cp := *t
		updatedTarget = &cp
	}
	m.mu.Unlock()

	statusMsg := fmt.Sprintf("Target URL: %s (Status: %s)", resp.URL, resp.Status)
	m.recordLog(target, cmdStr, duration, "OK", statusMsg)

	if updatedTarget != nil {
		m.Broadcast(Event{
			Type:      EventDisplayUpdated,
			Timestamp: time.Now(),
			Target:    target,
			Data:      updatedTarget,
		})
		m.broadcastStats()
	}

	return resp, nil
}

// GetURL requests the active browser URL from the target display.
func (m *Manager) GetURL(ctx context.Context, target string) (string, error) {
	if m.client == nil {
		return "", errors.New("display client not initialized")
	}

	start := time.Now()
	activeURL, err := m.client.GetURL(ctx, target)
	duration := time.Since(start)

	if err != nil {
		m.recordLog(target, "geturl", duration, "ERROR", err.Error())
		return "", err
	}

	var updatedTarget *DisplayTarget
	m.mu.Lock()
	if t, exists := m.targets[target]; exists {
		t.CurrentURL = activeURL
		t.LastSeen = time.Now()
		cp := *t
		updatedTarget = &cp
	}
	m.mu.Unlock()

	m.recordLog(target, "geturl", duration, "OK", activeURL)

	if updatedTarget != nil {
		m.Broadcast(Event{
			Type:      EventDisplayUpdated,
			Timestamp: time.Now(),
			Target:    target,
			Data:      updatedTarget,
		})
		m.broadcastStats()
	}

	return activeURL, nil
}

// GetStatus queries telemetry and status from the target display.
func (m *Manager) GetStatus(ctx context.Context, target string) (*rest.DashboardData, error) {
	if m.client == nil {
		return nil, errors.New("display client not initialized")
	}

	start := time.Now()
	status, err := m.client.GetStatus(ctx, target)
	duration := time.Since(start)

	if err != nil {
		m.recordLog(target, "getstatus", duration, "ERROR", err.Error())
		return nil, err
	}

	var updatedTarget *DisplayTarget
	m.mu.Lock()
	if t, exists := m.targets[target]; exists {
		t.LastStatus = status
		t.LastSeen = time.Now()
		if status.BrowserConfig.Address != "" && t.CurrentURL == "" {
			t.CurrentURL = status.BrowserConfig.Address
		}
		cp := *t
		updatedTarget = &cp
	}
	m.mu.Unlock()

	statusMsg := fmt.Sprintf("Uptime: %s, Viewport: %dx%d", status.Uptime, status.BrowserConfig.Width, status.BrowserConfig.Height)
	m.recordLog(target, "getstatus", duration, "OK", statusMsg)

	if updatedTarget != nil {
		m.Broadcast(Event{
			Type:      EventDisplayUpdated,
			Timestamp: time.Now(),
			Target:    target,
			Data:      updatedTarget,
		})
		m.broadcastStats()
	}

	return status, nil
}

// CaptureScreenshot captures and caches a screenshot from the target display.
func (m *Manager) CaptureScreenshot(ctx context.Context, target string, opts ...displayclient.ScreenshotOptions) (*ScreenshotItem, error) {
	if m.client == nil {
		return nil, errors.New("display client not initialized")
	}

	start := time.Now()
	result, err := m.client.GetScreenshot(ctx, target, opts...)
	duration := time.Since(start)

	if err != nil {
		m.recordLog(target, "screenshot", duration, "ERROR", err.Error())
		return nil, err
	}

	item := m.StoreScreenshot(target, result.Filename, result.MimeType, result.Data)
	msg := fmt.Sprintf("Saved %s (%d bytes, %s)", item.Filename, len(item.Data), item.MimeType)
	m.recordLog(target, "screenshot", duration, "OK", msg)
	return item, nil
}

// NavigateAll dispatches a navigation command to all known display targets concurrently.
func (m *Manager) NavigateAll(ctx context.Context, url string, selector ...string) map[string]error {
	displays := m.GetDisplays()
	errs := make(map[string]error)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, d := range displays {
		wg.Add(1)
		go func(targetID string) {
			defer wg.Done()
			_, err := m.Navigate(ctx, targetID, url, selector...)
			if err != nil {
				mu.Lock()
				errs[targetID] = err
				mu.Unlock()
			}
		}(d.ID)
	}
	wg.Wait()
	return errs
}

// RefreshAll queries status for all known display targets concurrently.
func (m *Manager) RefreshAll(ctx context.Context) map[string]error {
	displays := m.GetDisplays()
	errs := make(map[string]error)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, d := range displays {
		wg.Add(1)
		go func(targetID string) {
			defer wg.Done()
			_, err := m.GetStatus(ctx, targetID)
			if err != nil {
				mu.Lock()
				errs[targetID] = err
				mu.Unlock()
			}
		}(d.ID)
	}
	wg.Wait()
	return errs
}

// CaptureAll captures screenshots for all known display targets concurrently.
func (m *Manager) CaptureAll(ctx context.Context, opts ...displayclient.ScreenshotOptions) map[string]error {
	displays := m.GetDisplays()
	errs := make(map[string]error)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, d := range displays {
		wg.Add(1)
		go func(targetID string) {
			defer wg.Done()
			_, err := m.CaptureScreenshot(ctx, targetID, opts...)
			if err != nil {
				mu.Lock()
				errs[targetID] = err
				mu.Unlock()
			}
		}(d.ID)
	}
	wg.Wait()
	return errs
}

func generateID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(b))
}
