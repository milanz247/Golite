package middleware

import (
	"net/http"

	apphttp "Golite/app/Http"
	gosession "Golite/app/Http/Session"
	"Golite/auth"
	"Golite/encryption"
)

// Auth is Golite's session-based authentication middleware — the
// equivalent of Laravel's "auth" middleware. A request with no
// authenticated session is given one more chance via the "remember me"
// cookie (see auth.DecodeRememberCookie) before being rejected with 401
// — the same fallback Laravel's own SessionGuard performs internally.
type Auth struct {
	guard     *auth.Guard
	encrypter *encryption.Encrypter
}

// NewAuth builds an Auth middleware backed by guard and encrypter (the
// same *encryption.Encrypter AuthController.Login used to set the
// remember-me cookie — see auth.EncodeRememberCookie's doc comment for
// why it's specifically this one, not Context.SetCookie's ephemeral
// per-process key).
func NewAuth(guard *auth.Guard, encrypter *encryption.Encrypter) *Auth {
	return &Auth{guard: guard, encrypter: encrypter}
}

// Handle rejects the request with 401 unless the session already has an
// authenticated user, or a valid remember-me cookie re-establishes one.
func (a *Auth) Handle(c *apphttp.Context, next func(), _ ...string) {
	sess := c.Session()

	if !a.guard.Check(sess) && !a.attemptRememberLogin(c, sess) {
		c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}

	next()
}

// attemptRememberLogin looks for the remember-me cookie set at login
// (see AuthController.Login), decrypts and validates it against the
// stored token hash, and — if valid — transparently re-establishes a
// full session.
func (a *Auth) attemptRememberLogin(c *apphttp.Context, sess *gosession.Session) bool {
	raw, err := c.Request.Cookie(auth.RememberCookieName)
	if err != nil {
		return false
	}

	userID, token, err := auth.DecodeRememberCookie(a.encrypter, raw.Value)
	if err != nil {
		return false
	}

	user, err := a.guard.UserByRememberToken(userID, token)
	if err != nil {
		return false
	}

	a.guard.Login(sess, user)
	return true
}
