package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/je4/wsbrokerdisplayclient/pkg/manager"
)

func (s *Server) setupRoutes() {
	s.router.GET("/ping", s.handlePing)
	s.router.GET("/", s.handleDashboard)
	s.router.GET("/dashboard", s.handleDashboard)

	// Real-time Event Stream and Metrics
	s.router.GET("/api/events", s.handleEvents)
	s.router.GET("/api/stats", s.handleGetStats)

	// Display List and Card Actions
	s.router.GET("/displays", s.handleGetDisplays)
	s.router.POST("/api/displays/navigate", s.handleNavigateForm)
	s.router.POST("/api/displays/:id/navigate", s.handleNavigateTarget)
	s.router.POST("/api/displays/:id/geturl", s.handleGetURL)
	s.router.POST("/api/displays/:id/status", s.handleGetStatus)
	s.router.POST("/api/displays/:id/screenshot", s.handleCaptureScreenshot)

	// Bulk Operations
	s.router.POST("/api/displays/bulk/navigate", s.handleBulkNavigate)
	s.router.POST("/api/displays/bulk/status", s.handleBulkStatus)
	s.router.POST("/api/displays/bulk/screenshot", s.handleBulkScreenshot)

	// Display Targets Management
	s.router.POST("/api/targets/add", s.handleAddTarget)
	s.router.DELETE("/api/targets/:id", s.handleDeleteTarget)
	s.router.POST("/api/targets/:id/delete", s.handleDeleteTarget)

	// Modals & Lightboxes
	s.router.GET("/modal/navigate", s.handleModalNavigate)
	s.router.GET("/modal/target/add", s.handleModalAddTarget)
	s.router.GET("/modal/screenshot/:id", s.handleModalScreenshot)

	// Raw Binary Screenshot Serving
	s.router.GET("/screenshots/:id/raw", s.handleScreenshotRaw)

	// Command History
	s.router.GET("/history", s.handleGetHistory)
	s.router.POST("/history/clear", s.handleClearHistory)
}

func (s *Server) handlePing(c *gin.Context) {
	c.String(http.StatusOK, "pong")
}

func (s *Server) handleDashboard(c *gin.Context) {
	stats := s.manager.GetStats()
	c.HTML(http.StatusOK, "dashboard.gohtml", gin.H{
		"ClientName": s.clientName,
		"BrokerURL":  s.brokerURL,
		"ListenAddr": s.listenAddr,
		"Year":       time.Now().Year(),
		"Stats":      stats,
		"Displays":   s.manager.GetDisplays(),
		"History":    s.manager.GetHistory(),
	})
}

func (s *Server) handleEvents(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	eventCh := s.manager.Subscribe(c.Request.Context())

	// Send initial stats on connect
	stats := s.manager.GetStats()
	if statsJSON, err := json.Marshal(stats); err == nil {
		fmt.Fprintf(c.Writer, "event: stats_updated\ndata: %s\n\n", string(statsJSON))
		c.Writer.Flush()
	}

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case ev, ok := <-eventCh:
			if !ok {
				return
			}
			evJSON, err := json.Marshal(ev.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", ev.Type, string(evJSON))
			c.Writer.Flush()
		}
	}
}

func (s *Server) handleGetStats(c *gin.Context) {
	stats := s.manager.GetStats()
	c.JSON(http.StatusOK, stats)
}

func (s *Server) handleGetDisplays(c *gin.Context) {
	displays := s.manager.GetDisplays()
	c.HTML(http.StatusOK, "cards.gohtml", gin.H{
		"Displays": displays,
	})
}

func (s *Server) handleGetURL(c *gin.Context) {
	targetID := c.Param("id")
	_, _ = s.manager.GetURL(c.Request.Context(), targetID)

	target, exists := s.manager.GetTarget(targetID)
	if !exists {
		c.String(http.StatusNotFound, "Display target not found")
		return
	}
	c.HTML(http.StatusOK, "display_card.gohtml", target)
}

func (s *Server) handleGetStatus(c *gin.Context) {
	targetID := c.Param("id")
	_, _ = s.manager.GetStatus(c.Request.Context(), targetID)

	target, exists := s.manager.GetTarget(targetID)
	if !exists {
		c.String(http.StatusNotFound, "Display target not found")
		return
	}
	c.HTML(http.StatusOK, "display_card.gohtml", target)
}

func (s *Server) handleCaptureScreenshot(c *gin.Context) {
	targetID := c.Param("id")
	_, _ = s.manager.CaptureScreenshot(c.Request.Context(), targetID)

	target, exists := s.manager.GetTarget(targetID)
	if !exists {
		c.String(http.StatusNotFound, "Display target not found")
		return
	}
	c.HTML(http.StatusOK, "display_card.gohtml", target)
}

func (s *Server) handleNavigateTarget(c *gin.Context) {
	targetID := c.Param("id")
	targetURL := c.PostForm("url")
	selector := c.PostForm("selector")

	if targetURL == "" {
		c.String(http.StatusBadRequest, "Missing url parameter")
		return
	}

	_, _ = s.manager.Navigate(c.Request.Context(), targetID, targetURL, selector)

	target, exists := s.manager.GetTarget(targetID)
	if !exists {
		c.String(http.StatusNotFound, "Display target not found")
		return
	}
	c.HTML(http.StatusOK, "display_card.gohtml", target)
}

func (s *Server) handleBulkNavigate(c *gin.Context) {
	targetURL := c.PostForm("url")
	selector := c.PostForm("selector")

	if targetURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}

	errs := s.manager.NavigateAll(c.Request.Context(), targetURL, selector)
	if len(errs) > 0 {
		msg := fmt.Sprintf("Bulk navigation dispatched with %d error(s)", len(errs))
		s.manager.BroadcastToast("info", "Bulk Navigation", msg)
		c.JSON(http.StatusOK, gin.H{"status": "partial", "errors": len(errs)})
		return
	}

	s.manager.BroadcastToast("success", "Bulk Navigation", fmt.Sprintf("Dispatched %s to all displays", targetURL))
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) handleBulkStatus(c *gin.Context) {
	errs := s.manager.RefreshAll(c.Request.Context())
	if len(errs) > 0 {
		s.manager.BroadcastToast("info", "Bulk Status Refresh", fmt.Sprintf("Refreshed with %d error(s)", len(errs)))
	} else {
		s.manager.BroadcastToast("success", "Bulk Status Refresh", "Status refreshed for all displays")
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "errors": len(errs)})
}

func (s *Server) handleBulkScreenshot(c *gin.Context) {
	errs := s.manager.CaptureAll(c.Request.Context())
	if len(errs) > 0 {
		s.manager.BroadcastToast("info", "Bulk Screenshot", fmt.Sprintf("Capture completed with %d error(s)", len(errs)))
	} else {
		s.manager.BroadcastToast("success", "Bulk Screenshot", "Screenshots captured for all displays")
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "errors": len(errs)})
}

func (s *Server) handleNavigateForm(c *gin.Context) {
	targetID := c.PostForm("target")
	targetURL := c.PostForm("url")
	selector := c.PostForm("selector")

	if targetID == "" || targetURL == "" {
		c.String(http.StatusBadRequest, "Target and URL are required")
		return
	}

	_, err := s.manager.Navigate(c.Request.Context(), targetID, targetURL, selector)
	if err != nil {
		c.Header("HX-Retarget", "#modal-container")
		c.String(http.StatusOK, fmt.Sprintf(`<div class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-950/80"><div class="bg-slate-900 border border-red-500/50 rounded-xl p-6 text-center space-y-4"><p class="text-red-400 font-semibold">Navigation Failed: %s</p><button onclick="document.getElementById('modal-container').innerHTML=''" class="px-4 py-2 bg-slate-800 text-white rounded-lg">Close</button></div></div>`, err.Error()))
		return
	}

	// Trigger displays reload and close modal
	c.Header("HX-Trigger", "refresh-displays")
	c.String(http.StatusOK, `<div class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-950/80 animate-fade-in"><div class="bg-slate-900 border border-emerald-500/50 rounded-xl p-6 text-center space-y-4"><p class="text-emerald-400 font-semibold">Navigation Dispatched Successfully!</p><button onclick="document.getElementById('modal-container').innerHTML=''" class="px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-sm font-medium">Dismiss</button></div></div>`)
}

func (s *Server) handleAddTarget(c *gin.Context) {
	id := c.PostForm("id")
	name := c.PostForm("name")
	channel := c.PostForm("channel")

	if id != "" {
		s.manager.AddTarget(id, name, channel)
		s.manager.BroadcastToast("success", "Target Registered", fmt.Sprintf("Display target '%s' registered", id))
	}

	displays := s.manager.GetDisplays()
	c.HTML(http.StatusOK, "cards.gohtml", gin.H{
		"Displays": displays,
	})
}

func (s *Server) handleDeleteTarget(c *gin.Context) {
	targetID := c.Param("id")
	if targetID == "" {
		c.String(http.StatusBadRequest, "Missing target id")
		return
	}

	removed := s.manager.RemoveTarget(targetID)
	if !removed {
		c.String(http.StatusNotFound, "Target not found")
		return
	}

	s.manager.BroadcastToast("info", "Target Removed", fmt.Sprintf("Display target %s removed", targetID))

	if c.GetHeader("HX-Request") != "" {
		c.Status(http.StatusOK)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted", "id": targetID})
}

func (s *Server) handleModalNavigate(c *gin.Context) {
	target := c.Query("target")
	c.HTML(http.StatusOK, "command_modal.gohtml", gin.H{
		"Target": target,
	})
}

func (s *Server) handleModalAddTarget(c *gin.Context) {
	c.HTML(http.StatusOK, "target_modal.gohtml", gin.H{})
}

func (s *Server) handleModalScreenshot(c *gin.Context) {
	screenshotID := c.Param("id")
	item, exists := s.manager.GetScreenshot(screenshotID)
	if !exists {
		c.String(http.StatusNotFound, "Screenshot not found")
		return
	}

	c.HTML(http.StatusOK, "screenshot_viewer.gohtml", item)
}

func (s *Server) handleScreenshotRaw(c *gin.Context) {
	screenshotID := c.Param("id")
	item, exists := s.manager.GetScreenshot(screenshotID)
	if !exists {
		c.String(http.StatusNotFound, "Screenshot not found")
		return
	}

	c.Data(http.StatusOK, item.MimeType, item.Data)
}

func (s *Server) handleGetHistory(c *gin.Context) {
	entries := s.manager.GetHistory()
	c.HTML(http.StatusOK, "history.gohtml", gin.H{
		"Entries": entries,
	})
}

func (s *Server) handleClearHistory(c *gin.Context) {
	s.manager.ClearHistory()
	s.manager.BroadcastToast("info", "History Cleared", "Command execution history was reset")
	c.HTML(http.StatusOK, "history.gohtml", gin.H{
		"Entries": []manager.CommandLogEntry{},
	})
}
