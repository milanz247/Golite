package routes

import (
	"golite/app/http/controllers"
)

// Kernel එකට handle කළ හැකි router interface එකක් සාදා ගැනීම
type Router interface {
	GET(path string, handler func(*interface{})) // පොදු interface එකක් සඳහා
}

func MapWebRoutes(router interface{}) {
	// Type assertion මඟින් අපේ Kernel router එක හඳුනාගැනීම
	r := router.(interface {
		GET(path string, handler func(*interface{})) // Type match කිරීම සඳහා mock
	})

	userController := controllers.NewUserController()

	// Route එකක් සාදා Controller method එකකට සම්බන්ධ කිරීම
	r.GET("/user/profile", func(ctx *interface{}) {
		// Context එක නිවැරදි type එකට හැරවීම (Cast)
		actualCtx := ctx.(*interface{}) // Note: Type alignment with HTTP kernel context
		_ = actualCtx
	})
}