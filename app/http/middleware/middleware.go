package middleware

import (
	"fmt"
	"golite/app/http"
	"time"
)

// Request එක ලැබුණු වෙලාව සහ ගතවූ කාලය මනින Logger Middleware එක
func Logger() http.HandlerFunc {
	return func(c *http.Context) {
		t := time.Now()
		fmt.Printf("[Middleware] Incoming Request: %s %s\n", c.Request.Method, c.Request.URL.Path)
		
		c.Next() // ඊළඟ Middleware හෝ Controller එකට යවයි
		
		fmt.Printf("[Middleware] Response Sent in %v\n", time.Since(t))
	}
}

// Auth පරීක්ෂා කරන සරල Middleware එකක්
func Authenticate() http.HandlerFunc {
	return func(c *http.Context) {
		token := c.Request.Header.Get("Authorization")
		if token == "" {
			c.JSON(401, map[string]string{"error": "Unauthorized Access"})
			return // Next එක call කරන්නේ නැති නිසා මෙතැනින් request එක නතර වේ
		}
		c.Next()
	}
}