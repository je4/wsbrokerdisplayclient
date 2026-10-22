// Package displayclient provides a clean, idiomatic Go client library for remotely
// controlling WSBroker display instances over WebSocket connections.
// It supports navigation, URL inspection, resolution-bounded screenshots, and telemetry/status queries.
package displayclient

import (
	"time"

	"github.com/je4/wsbrokerdisplay/pkg/rest"
)

// Event constants for display remote control commands and response envelopes.
const (
	// EventNavigate identifies a command requesting browser navigation to a target URL.
	EventNavigate = "display:navigate"
	// EventNavigateResponse identifies a response acknowledgment for a navigation command.
	EventNavigateResponse = "display:navigate_response"
	// EventGetURL identifies a command requesting the active browser page URL.
	EventGetURL = "display:geturl"
	// EventGetURLResponse identifies a response containing the active browser page URL.
	EventGetURLResponse = "display:geturl_response"
	// EventGetScreenshot identifies a command requesting a bounded viewport screenshot.
	EventGetScreenshot = "display:getscreenshot"
	// EventScreenshotResponse identifies an error or status response for a screenshot request.
	EventScreenshotResponse = "display:getscreenshot_response"
	// EventGetStatus identifies a command requesting live display telemetry.
	EventGetStatus = "display:getstatus"
	// EventGetStatusResponse identifies a response containing display telemetry.
	EventGetStatusResponse = "display:getstatus_response"
)

// Action constants for legacy display commands and responses.
const (
	// ActionNavigate identifies a legacy command requesting browser navigation.
	ActionNavigate = "navigate"
	// ActionNavigateResponse identifies a legacy response for navigation.
	ActionNavigateResponse = "navigate_response"
	// ActionGetURL identifies a legacy command requesting the active page URL.
	ActionGetURL = "geturl"
	// ActionGetURLResponse identifies a legacy response containing the active URL.
	ActionGetURLResponse = "geturl_response"
	// ActionGetScreenshot identifies a legacy command requesting a screenshot.
	ActionGetScreenshot = "getscreenshot"
	// ActionScreenshotResponse identifies a legacy error or status response for a screenshot.
	ActionScreenshotResponse = "getscreenshot_response"
	// ActionGetStatus identifies a legacy command requesting display telemetry.
	ActionGetStatus = "getstatus"
	// ActionGetStatusResponse identifies a legacy response containing display telemetry.
	ActionGetStatusResponse = "getstatus_response"
)

// CommandMessage represents an outgoing command request payload sent to the display service via WSBroker.
type CommandMessage struct {
	// Event specifies the event operation name (e.g. "display:navigate", "display:getstatus").
	Event string `json:"event,omitempty"`
	// Action specifies the legacy operation name ("navigate", "getscreenshot", "geturl", "getstatus").
	Action string `json:"action,omitempty"`
	// URL specifies the target web address for navigation commands.
	URL string `json:"url,omitempty"`
	// Selector specifies an optional CSS query selector to wait for after navigation.
	Selector string `json:"selector,omitempty"`
	// Format specifies the desired screenshot format ("png" or "jpeg", default: "jpeg").
	Format string `json:"format,omitempty"`
	// Quality specifies the JPEG compression quality (1-100, default: 85).
	Quality int `json:"quality,omitempty"`
}

// NavigateResponse represents the response envelope returned for a navigation command.
type NavigateResponse struct {
	// Event specifies the event identifier if present ("display:navigate_response").
	Event string `json:"event,omitempty"`
	// Action specifies the legacy action identifier if present ("navigate_response").
	Action string `json:"action,omitempty"`
	// Status indicates the operation outcome ("ok" or "error").
	Status string `json:"status"`
	// URL contains the target URL that was requested.
	URL string `json:"url"`
	// Error contains an error description if navigation failed.
	Error string `json:"error,omitempty"`
}

// URLResponse represents the response envelope returned for an active URL query.
type URLResponse struct {
	// Event specifies the event identifier if present ("display:geturl_response").
	Event string `json:"event,omitempty"`
	// Action specifies the legacy action identifier if present ("geturl_response").
	Action string `json:"action,omitempty"`
	// URL contains the current primary browser location URL.
	URL string `json:"url"`
	// Error contains an error message if the URL inspection failed.
	Error string `json:"error,omitempty"`
}

// ScreenshotResponse represents an error or status response when screenshot capture fails.
type ScreenshotResponse struct {
	// Event specifies the event identifier if present ("display:getscreenshot_response").
	Event string `json:"event,omitempty"`
	// Action specifies the legacy action identifier if present ("getscreenshot_response").
	Action string `json:"action,omitempty"`
	// Status indicates operation outcome ("ok" or "error").
	Status string `json:"status"`
	// Error contains an error description if screenshot capture failed.
	Error string `json:"error,omitempty"`
}

// StatusResponse represents the response envelope containing display telemetry or error details.
type StatusResponse struct {
	// Event specifies the event identifier if present ("display:getstatus_response").
	Event string `json:"event,omitempty"`
	// Action specifies the legacy action identifier if present ("getstatus_response").
	Action string `json:"action,omitempty"`
	// Status indicates operation outcome ("ok" or "error").
	Status string `json:"status"`
	// Data contains the telemetry dashboard data when status is "ok".
	Data *rest.DashboardData `json:"data,omitempty"`
	// Error contains an error description if status retrieval failed.
	Error string `json:"error,omitempty"`
}

// ScreenshotOptions configures optional parameters for screenshot capture requests.
type ScreenshotOptions struct {
	// Format is the image format ("jpeg" or "png", default: "jpeg").
	Format string
	// Quality is the JPEG compression quality (1-100, default: 85).
	Quality int
}

// ScreenshotResult contains the captured screenshot image binary data and metadata.
type ScreenshotResult struct {
	// Filename is the generated or received filename of the screenshot (e.g. "screenshot.jpg").
	Filename string
	// MimeType is the MIME type of the image (e.g. "image/jpeg" or "image/png").
	MimeType string
	// Data contains the raw binary image bytes.
	Data []byte
}

// Option defines a functional option for configuring a Client.
type Option func(*Client)

// WithTimeout sets a default per-request timeout for synchronous calls when context has no deadline.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.defaultTimeout = d
		}
	}
}

// WithDefaultScreenshotOptions sets the default screenshot format and quality.
func WithDefaultScreenshotOptions(opts ScreenshotOptions) Option {
	return func(c *Client) {
		if opts.Format != "" {
			c.defaultScreenshotOpts.Format = opts.Format
		}
		if opts.Quality > 0 {
			c.defaultScreenshotOpts.Quality = opts.Quality
		}
	}
}
