package main 

import (
    "github.com/gofiber/fiber/v2"
    "github.com/neuroguard/gateway/auth"
    "github.com/neuroguard/gateway/handler"
)

func main() {
	app := fiber.New()

	// Authentication middleware
	authSvc := auth.NewService(os.Getenv("JWT_SECRET"))

	app.Post("/auth/login",   handler.Login(authSvc))
    app.Post("/auth/logout",  handler.Logout(authSvc))
    app.Post("/crisis/aura",  authSvc.Protect(handler.TriggerAura))
    app.Post("/crisis/confirm", authSvc.Protect(handler.ConfirmRescue))

	app.listen(":8080")

}