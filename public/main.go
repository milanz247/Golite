package main

import (
	"fmt"
	"golite/bootstrap"
	"golite/app/http"
	"golite/app/http/middleware"
	"golite/app/providers"
	netHttp "net/http"
)

func main() {
	fmt.Println("--- Starting Golite Framework ---")

	// 1. App Instance එක සහ Service Container එක නිර්මාණය කිරීම (bootstrap/app.php)
	app := bootstrap.NewApplication()

	// 2. Service Providers ලියාපදිංචි කිරීම
	app.RegisterProvider(&providers.AppServiceProvider{})
	app.RegisterProvider(&providers.RouteServiceProvider{})

	// 3. Providers සියල්ල සක්‍රීය (Boot) කිරීම
	app.BootProviders()

	// 4. Kernel එක Container එකෙන් ලබා ගැනීම
	kernel := app.Container.Make("kernel").(*http.Kernel)

	// 5. Global Middlewares එකතු කිරීම
	kernel.Use(middleware.Logger())

	// 6. සර්වර් එක Port :8080 ඔස්සේ ක්‍රියාත්මක කිරීම
	fmt.Println("[Golite Engine] Listening and Serving HTTP on :8080")
	err := netHttp.ListenAndServe(":8080", kernel)
	if err != nil {
		panic(err)
	}
}