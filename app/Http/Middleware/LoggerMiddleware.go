package middleware

import (
	"log"
	"time"

	apphttp "Golite/app/Http"
)

// Logger logs the request method, path, and how long it took to complete,
// mirroring the kind of request logging Laravel ships via middleware.
func Logger() apphttp.HandlerFunc {
	return func(c *apphttp.Context) {
		start := time.Now()
		method := c.Request.Method
		path := c.Request.URL.Path

		c.Next()

		log.Printf("%s %s completed in %s", method, path, time.Since(start))
	}
}
