package controllers

import (
	"golite/app/http"
)

type UserController struct{}

func NewUserController() *UserController {
	return &UserController{}
}

// User Profile එක පෙන්වන Controller Method එක
func (u *UserController) Show(c *http.Context) {
	// Service Container එකෙන් Database connection තොරතුරු ලබා ගැනීම
	db := c.Container.Make("db_connection_string").(string)

	c.JSON(200, map[string]interface{}{
		"username": "Kasun Perera",
		"email":    "kasun@example.com",
		"source":   "Loaded using Golite IoC Container",
		"db_info":  db,
	})
}