package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/logging"
)

// LogController demonstrates the logging package (see docs/logging.md):
// writing leveled entries via a method-injected logging.Logger, resolved
// automatically by apphttp.Inject (see routes/web.go).
type LogController struct {
	Controller
}

// NewLogController creates a new LogController.
func NewLogController() *LogController {
	return &LogController{}
}

// Demo handles GET /logs/demo, writing one entry at each of a few levels.
// logger is resolved automatically from the container — Golite's
// equivalent of a Laravel action type-hinting LoggerInterface $logger.
func (lc *LogController) Demo(c *apphttp.Context, logger logging.Logger) {
	logger.Info("demo info entry", map[string]any{"ip": c.Ip()})
	logger.Warning("demo warning entry")
	logger.Error("demo error entry", map[string]any{"path": c.Path()})
	c.JSON(http.StatusOK, map[string]string{"status": "logged", "see": "storage/logs/golite.log"})
}
