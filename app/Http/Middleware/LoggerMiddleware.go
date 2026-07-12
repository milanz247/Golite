package middleware

import (
	"log"
	"time"

	apphttp "Golite/app/Http"
)

// Logger logs the request method, path, and how long it took to complete —
// an "after" style middleware: it runs its own code both before *and*
// after the rest of the chain, by capturing the start time, calling next,
// and only logging once next has returned (i.e. once everything downstream
// has finished).
func Logger() apphttp.Middleware {
	return apphttp.MiddlewareFunc(func(c *apphttp.Context, next func()) {
		start := time.Now()
		method := c.Request.Method
		path := c.Request.URL.Path

		next()

		log.Printf("%s %s completed in %s", method, path, time.Since(start))
	})
}
