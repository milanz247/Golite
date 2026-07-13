package controllers

import (
	"errors"
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/auth"
	"Golite/encryption"
)

// AuthController is Golite's authentication controller — register,
// login, logout, "remember me", and password reset, all built on the
// top-level auth package (see docs/authentication.md). Every action
// takes its *auth.Guard (and, for Login/Logout, *encryption.Encrypter
// too, for the remember-me cookie) as a method-injected parameter,
// resolved by apphttp.Inject the same way UserController.Show used to be
// (see docs/controllers.md#method-injection--apphttpinject) — wired up
// in routes/web.go's registerAuthRoutes.
type AuthController struct {
	Controller
}

// NewAuthController creates a new AuthController.
func NewAuthController() *AuthController {
	return &AuthController{}
}

// Register creates a new user, logs them in immediately, and returns the
// created user as JSON — matching Laravel's default
// RegisteredUserController behavior.
func (a *AuthController) Register(c *apphttp.Context, guard *auth.Guard) {
	validated := c.Validate(map[string]string{
		"name":     "required|string|min:2",
		"email":    "required|email",
		"password": "required|min:8|confirmed",
	})
	name, _ := validated["name"].(string)
	email, _ := validated["email"].(string)
	password, _ := validated["password"].(string)

	user, err := guard.Register(name, email, password)
	if err != nil {
		if errors.Is(err, auth.ErrEmailTaken) {
			c.JSON(http.StatusUnprocessableEntity, map[string]any{
				"message": "the given data was invalid",
				"errors":  map[string][]string{"email": {"This email is already registered."}},
			})
			return
		}
		panic(err) // an unexpected database failure, not a client-input problem -- RecoverMiddleware renders a 500
	}

	guard.Login(c.Session(), user)
	c.RegenerateSession() // prevent session fixation around this privilege change

	c.JSON(http.StatusCreated, map[string]any{"user": user})
}

// Login verifies credentials, logs the user in, and — if the client sent
// a truthy "remember" field — issues a persistent "remember me" cookie
// that survives well past the session's own lifetime (and, unlike the
// session cookie, a server restart too; see
// auth.EncodeRememberCookie's doc comment).
func (a *AuthController) Login(c *apphttp.Context, guard *auth.Guard, encrypter *encryption.Encrypter) {
	validated := c.Validate(map[string]string{
		"email":    "required|email",
		"password": "required",
	})
	email, _ := validated["email"].(string)
	password, _ := validated["password"].(string)

	user, err := guard.Attempt(email, password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, map[string]string{"error": "these credentials do not match our records"})
		return
	}

	guard.Login(c.Session(), user)
	c.RegenerateSession()

	if c.Boolean("remember") {
		// A failure issuing the remember-me cookie shouldn't fail the
		// login itself -- the user is still authenticated for this
		// session, they just won't be remembered past it.
		if token, err := guard.IssueRememberToken(user); err == nil {
			if cookieValue, err := auth.EncodeRememberCookie(encrypter, user.ID, token); err == nil {
				http.SetCookie(c.Writer, &http.Cookie{
					Name:     auth.RememberCookieName,
					Value:    cookieValue,
					Path:     "/",
					MaxAge:   auth.RememberCookieMaxAge,
					HttpOnly: true,
					Secure:   apphttp.IsSecureRequest(c.Request),
					SameSite: http.SameSiteLaxMode,
				})
			}
		}
	}

	c.JSON(http.StatusOK, map[string]any{"user": user})
}

// Logout clears the session and any remember-me token/cookie — the
// latter so a stolen-but-unused cookie value stops working immediately,
// not just once it expires.
func (a *AuthController) Logout(c *apphttp.Context, guard *auth.Guard) {
	if user := guard.User(c.Session()); user != nil {
		_ = guard.ClearRememberToken(user)
	}
	guard.Logout(c.Session())
	c.InvalidateSession()

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     auth.RememberCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	c.JSON(http.StatusOK, map[string]string{"status": "logged out"})
}

// Me returns the currently authenticated user — a protected route
// demonstrating the "auth" middleware (see
// app/Http/Middleware/AuthMiddleware.go and routes/web.go).
func (a *AuthController) Me(c *apphttp.Context, guard *auth.Guard) {
	user := guard.User(c.Session())
	if user == nil {
		c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}
	c.JSON(http.StatusOK, map[string]any{"user": user})
}

// ForgotPassword generates a password reset token for the given email.
func (a *AuthController) ForgotPassword(c *apphttp.Context, guard *auth.Guard) {
	validated := c.Validate(map[string]string{"email": "required|email"})
	email, _ := validated["email"].(string)

	token, err := guard.CreatePasswordResetToken(email)
	if err != nil {
		c.JSON(http.StatusNotFound, map[string]string{"error": "no account found for that email"})
		return
	}

	// There's no mail system in Golite yet (see
	// auth.Guard.CreatePasswordResetToken's doc comment) -- the token is
	// returned directly here, standing in for "emailed to the user". Do
	// not ship this response shape to a real deployment without wiring
	// up actual email delivery first.
	c.JSON(http.StatusOK, map[string]any{
		"status": "password reset token generated",
		"token":  token,
	})
}

// ResetPassword consumes a token from ForgotPassword to set a new
// password.
func (a *AuthController) ResetPassword(c *apphttp.Context, guard *auth.Guard) {
	validated := c.Validate(map[string]string{
		"email":    "required|email",
		"token":    "required",
		"password": "required|min:8|confirmed",
	})
	email, _ := validated["email"].(string)
	token, _ := validated["token"].(string)
	password, _ := validated["password"].(string)

	if err := guard.ResetPassword(email, token, password); err != nil {
		c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "invalid or expired token"})
		return
	}

	c.JSON(http.StatusOK, map[string]string{"status": "password reset"})
}
