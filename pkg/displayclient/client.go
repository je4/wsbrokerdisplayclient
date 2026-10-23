// Package displayclient provides a high-level client library for remotely controlling
// browser display units connected via the WSBroker WebSocket infrastructure.
// Callers can issue navigation commands, retrieve active URLs, capture resolution-bounded
// screenshots, and query telemetry data synchronously or asynchronously.
package displayclient

import (
	"context"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/je4/utils/v2/pkg/zLogger"
	rawclient "github.com/je4/wsbrokerclient/pkg/client"
	"github.com/je4/wsbrokerclient/pkg/event"
	"github.com/je4/wsbrokerdisplay/pkg/rest"
)

// BrokerClient abstracts the underlying WSBroker transport operations.
// Both *rawclient.Client and test mock implementations satisfy this interface.
type BrokerClient = rawclient.BrokerClient

// Client is a high-level remote display control client wrapping BrokerClient.
// It manages correlation tokens and response channels to provide synchronous
// and asynchronous APIs for controlling display browsers over WebSocket.
type Client struct {
	raw                   BrokerClient
	logger                zLogger.ZLogger
	mu                    sync.RWMutex
	closed                bool
	defaultTimeout        time.Duration
	defaultScreenshotOpts ScreenshotOptions
}

// New creates a new remote display control Client instance wrapping an existing WSBroker client.
// Returns an error if the raw client is nil.
func New(raw BrokerClient, logger zLogger.ZLogger, opts ...Option) (*Client, error) {
	if raw == nil {
		return nil, errors.New("raw wsbroker client cannot be nil")
	}

	c := &Client{
		raw:    raw,
		logger: logger,
		defaultScreenshotOpts: ScreenshotOptions{
			Format:  "jpeg",
			Quality: 85,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// applyTimeout returns a child context bounded by the default timeout if configured and no deadline is set.
func (c *Client) applyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.defaultTimeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			return context.WithTimeout(ctx, c.defaultTimeout)
		}
	}
	return ctx, func() {}
}

// RawClient returns the underlying raw WSBroker client instance.
func (c *Client) RawClient() BrokerClient {
	return c.raw
}

// ClientName returns the configured unique identifier of the underlying WSBroker client.
func (c *Client) ClientName() string {
	return c.raw.ClientName()
}

// Connect establishes the WebSocket connection to the broker asynchronously.
func (c *Client) Connect(ctx context.Context) error {
	return c.raw.Connect(ctx)
}

// ConnectSync establishes the WebSocket connection to the broker synchronously.
func (c *Client) ConnectSync(ctx context.Context) error {
	return c.raw.ConnectSync(ctx)
}

// Close gracefully closes the client, removes listeners, and closes the underlying broker connection.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	return c.raw.Close()
}

// OnEvent registers a listener callback for all incoming events.
func (c *Client) OnEvent(handler rawclient.EventHandler) rawclient.UnsubscribeFunc {
	return c.raw.OnEvent(handler)
}

// OnMessage registers a listener callback for incoming TypeMessage events.
func (c *Client) OnMessage(handler rawclient.EventHandler) rawclient.UnsubscribeFunc {
	return c.raw.OnMessage(handler)
}

// OnDirect registers a listener callback for events addressed directly to this client.
func (c *Client) OnDirect(handler rawclient.EventHandler) rawclient.UnsubscribeFunc {
	return c.raw.OnDirect(handler)
}

// OnChannel registers a listener callback for events broadcast to a channel.
func (c *Client) OnChannel(channel string, handler rawclient.EventHandler) rawclient.UnsubscribeFunc {
	return c.raw.OnChannel(channel, handler)
}

// Navigate sends a synchronous navigation command to the target display client and waits for acknowledgment.
// An optional waitSelector can be specified to wait until the DOM element is rendered.
func (c *Client) Navigate(ctx context.Context, target string, targetURL string, waitSelector ...string) (*NavigateResponse, error) {
	ctx, cancel := c.applyTimeout(ctx)
	defer cancel()

	selector := ""
	if len(waitSelector) > 0 {
		selector = waitSelector[0]
	}

	cmd := CommandMessage{
		Event:    EventNavigate,
		Action:   ActionNavigate,
		URL:      targetURL,
		Selector: selector,
	}

	evt, err := event.NewMessageEvent(c.raw.ClientName(), target, cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create navigate command event")
	}

	respEvt, err := c.raw.Request(ctx, evt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send navigate command")
	}
	if respEvt == nil {
		return nil, errors.New("client closed before response received")
	}

	var resp NavigateResponse
	if err := respEvt.UnmarshalData(&resp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal navigate response")
	}
	if resp.Status != "ok" && resp.Error != "" {
		return &resp, errors.Errorf("navigate error: %s", resp.Error)
	}
	return &resp, nil
}

// GetURL queries the current primary page location URL of the target display client synchronously.
func (c *Client) GetURL(ctx context.Context, target string) (string, error) {
	ctx, cancel := c.applyTimeout(ctx)
	defer cancel()

	cmd := CommandMessage{
		Event:  EventGetURL,
		Action: ActionGetURL,
	}

	evt, err := event.NewMessageEvent(c.raw.ClientName(), target, cmd)
	if err != nil {
		return "", errors.Wrap(err, "failed to create geturl command event")
	}

	respEvt, err := c.raw.Request(ctx, evt)
	if err != nil {
		return "", errors.Wrap(err, "failed to send geturl command")
	}
	if respEvt == nil {
		return "", errors.New("client closed before response received")
	}

	var resp URLResponse
	if err := respEvt.UnmarshalData(&resp); err != nil {
		return "", errors.Wrap(err, "failed to unmarshal geturl response")
	}
	if resp.Error != "" {
		return "", errors.Errorf("geturl error: %s", resp.Error)
	}
	return resp.URL, nil
}

// GetScreenshot requests a bounded viewport screenshot from the target display client synchronously.
// The resulting image is constrained to a maximum resolution of 800x600 pixels.
func (c *Client) GetScreenshot(ctx context.Context, target string, opts ...ScreenshotOptions) (*ScreenshotResult, error) {
	ctx, cancel := c.applyTimeout(ctx)
	defer cancel()

	opt := c.defaultScreenshotOpts
	if len(opts) > 0 {
		if opts[0].Format != "" {
			opt.Format = opts[0].Format
		}
		if opts[0].Quality > 0 {
			opt.Quality = opts[0].Quality
		}
	}
	if opt.Format == "" {
		opt.Format = "jpeg"
	}
	if opt.Quality <= 0 {
		opt.Quality = 85
	}

	cmd := CommandMessage{
		Event:   EventGetScreenshot,
		Action:  ActionGetScreenshot,
		Format:  opt.Format,
		Quality: opt.Quality,
	}

	evt, err := event.NewMessageEvent(c.raw.ClientName(), target, cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create getscreenshot command event")
	}

	respEvt, err := c.raw.Request(ctx, evt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send getscreenshot command")
	}
	if respEvt == nil {
		return nil, errors.New("client closed before response received")
	}

	if respEvt.Type == event.TypeFile {
		var fileMsg event.FileMessage
		if err := respEvt.UnmarshalData(&fileMsg); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal screenshot file message")
		}
		return &ScreenshotResult{
			Filename: fileMsg.Filename,
			MimeType: fileMsg.MimeType,
			Data:     fileMsg.Data,
		}, nil
	}

	// Check if an error message response was returned
	var resp ScreenshotResponse
	if err := respEvt.UnmarshalData(&resp); err == nil && resp.Error != "" {
		return nil, errors.Errorf("screenshot error: %s", resp.Error)
	}
	return nil, errors.New("unexpected response payload received for screenshot request")
}

// GetStatus queries the telemetry dashboard data of the target display client synchronously.
func (c *Client) GetStatus(ctx context.Context, target string) (*rest.DashboardData, error) {
	ctx, cancel := c.applyTimeout(ctx)
	defer cancel()

	cmd := CommandMessage{
		Event:  EventGetStatus,
		Action: ActionGetStatus,
	}

	evt, err := event.NewMessageEvent(c.raw.ClientName(), target, cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create getstatus command event")
	}

	respEvt, err := c.raw.Request(ctx, evt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send getstatus command")
	}
	if respEvt == nil {
		return nil, errors.New("client closed before response received")
	}

	var resp StatusResponse
	if err := respEvt.UnmarshalData(&resp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal getstatus response")
	}
	if resp.Status != "ok" && resp.Error != "" {
		return nil, errors.Errorf("getstatus error: %s", resp.Error)
	}
	if resp.Error != "" {
		return nil, errors.Errorf("getstatus error: %s", resp.Error)
	}
	if resp.Data == nil {
		return nil, errors.New("getstatus response missing data payload")
	}
	return resp.Data, nil
}

// SendNavigate transmits an asynchronous (fire-and-forget) navigation command.
func (c *Client) SendNavigate(ctx context.Context, target string, targetURL string, waitSelector string, token ...string) error {
	cmd := CommandMessage{
		Event:    EventNavigate,
		Action:   ActionNavigate,
		URL:      targetURL,
		Selector: waitSelector,
	}
	evt, err := event.NewMessageEvent(c.raw.ClientName(), target, cmd)
	if err != nil {
		return errors.Wrap(err, "failed to create navigate command event")
	}
	if len(token) > 0 && token[0] != "" {
		evt.Token = token[0]
	}
	return c.raw.Send(ctx, evt)
}

// SendGetURL transmits an asynchronous (fire-and-forget) geturl command.
func (c *Client) SendGetURL(ctx context.Context, target string, token ...string) error {
	cmd := CommandMessage{
		Event:  EventGetURL,
		Action: ActionGetURL,
	}
	evt, err := event.NewMessageEvent(c.raw.ClientName(), target, cmd)
	if err != nil {
		return errors.Wrap(err, "failed to create geturl command event")
	}
	if len(token) > 0 && token[0] != "" {
		evt.Token = token[0]
	}
	return c.raw.Send(ctx, evt)
}

// SendGetScreenshot transmits an asynchronous (fire-and-forget) getscreenshot command.
func (c *Client) SendGetScreenshot(ctx context.Context, target string, format string, quality int, token ...string) error {
	cmd := CommandMessage{
		Event:   EventGetScreenshot,
		Action:  ActionGetScreenshot,
		Format:  format,
		Quality: quality,
	}
	evt, err := event.NewMessageEvent(c.raw.ClientName(), target, cmd)
	if err != nil {
		return errors.Wrap(err, "failed to create getscreenshot command event")
	}
	if len(token) > 0 && token[0] != "" {
		evt.Token = token[0]
	}
	return c.raw.Send(ctx, evt)
}

// SendGetStatus transmits an asynchronous (fire-and-forget) getstatus command.
func (c *Client) SendGetStatus(ctx context.Context, target string, token ...string) error {
	cmd := CommandMessage{
		Event:  EventGetStatus,
		Action: ActionGetStatus,
	}
	evt, err := event.NewMessageEvent(c.raw.ClientName(), target, cmd)
	if err != nil {
		return errors.Wrap(err, "failed to create getstatus command event")
	}
	if len(token) > 0 && token[0] != "" {
		evt.Token = token[0]
	}
	return c.raw.Send(ctx, evt)
}
