package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/logging"
)

// LogController demonstrates the logging package (see docs/logging.md):
// writing leveled entries via a constructor-injected logging.Logger.
type LogController struct {
	Controller
	logger logging.Logger
}

// NewLogController constructs a LogController with an injected
// logging.Logger, resolved from the container's "log" binding in
// routes/web.go.
func NewLogController(logger logging.Logger) *LogController {
	return &LogController{logger: logger}
}

// Demo handles GET /logs/demo, writing one entry at each of a few levels.
func (lc *LogController) Demo(c *apphttp.Context) {
	lc.logger.Info("demo info entry", map[string]any{"ip": c.Ip()})
	lc.logger.Warning("demo warning entry")
	lc.logger.Error("demo error entry", map[string]any{"path": c.Path()})
	c.JSON(http.StatusOK, map[string]string{"status": "logged", "see": "storage/logs/golite.log"})
}
