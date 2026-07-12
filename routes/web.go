package routes

import (
	apphttp "Golite/app/Http"
	"Golite/app/Http/Controllers"
)

// MapWebRoutes registers the application's web routes onto the kernel,
// mirroring the route definitions found in Laravel's routes/web.php.
func MapWebRoutes(kernel *apphttp.Kernel) {
	userController := controllers.NewUserController()

	kernel.GET("/user", userController.Show)
}
