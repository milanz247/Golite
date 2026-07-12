package middleware

import (
	"log"
	"time"

	apphttp "Golite/app/Http"
)

const auditStartKey = "audit.start"

// Audit demonstrates Terminable middleware: it implements both Middleware
// (Handle) and TerminableMiddleware (Terminate). Handle runs inline with
// the request, recording a start time; Terminate runs only after the whole
// middleware/handler chain has finished writing the response, which is the
// right place for work that shouldn't delay the response the client sees —
// flushing an audit trail, emitting metrics, and the like.
//
// Handle stores the start time on the Context via Set, never as a field on
// *Audit itself: one shared Audit instance handles every concurrent
// request, so any per-request state has to live on the (per-request)
// Context instead, or it would be a data race.
type Audit struct{}

// NewAudit constructs an Audit middleware.
func NewAudit() *Audit {
	return &Audit{}
}

// Handle records when the request started and continues the chain.
func (m *Audit) Handle(c *apphttp.Context, next func(), _ ...string) {
	c.Set(auditStartKey, time.Now())
	next()
}

// Terminate runs after the response has been fully written, logging how
// long the whole request took — a stand-in for real post-response work
// like persisting an audit record or shipping metrics.
func (m *Audit) Terminate(c *apphttp.Context) {
	start, ok := c.Get(auditStartKey)
	if !ok {
		return
	}
	log.Printf("[audit] %s %s finished in %s", c.Request.Method, c.Request.URL.Path, time.Since(start.(time.Time)))
}
