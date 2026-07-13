package auth

import (
	"fmt"
	"strconv"
	"strings"

	models "Golite/app/Models"
	"Golite/encryption"
)

// RememberCookieName is the cookie AuthController.Login sets (when the
// client requests "remember me") and AuthMiddleware reads to
// transparently re-establish a session — exported so both live in
// exactly one place, not duplicated between the controller and the
// middleware.
const RememberCookieName = "remember_web"

// RememberCookieMaxAge is 30 days, in seconds — Laravel's own default
// remember-me cookie lifetime.
const RememberCookieMaxAge = 60 * 60 * 24 * 30

// IssueRememberToken generates a fresh remember-me token for user,
// storing only its hash (see newRandomToken) and returning the raw value
// — the caller (AuthController.Login) is responsible for encoding it
// into a cookie via EncodeRememberCookie.
func (g *Guard) IssueRememberToken(user *models.User) (string, error) {
	raw, hash, err := newRandomToken()
	if err != nil {
		return "", err
	}
	if err := g.db.Model(user).Update("remember_token", hash).Error; err != nil {
		return "", err
	}
	user.RememberToken = hash
	return raw, nil
}

// UserByRememberToken resolves userID and checks rawToken against its
// stored hash, returning ErrInvalidRememberToken if the user has no
// token issued (never, or cleared by logout) or it doesn't match.
func (g *Guard) UserByRememberToken(userID uint, rawToken string) (*models.User, error) {
	var user models.User
	if err := g.db.First(&user, userID).Error; err != nil {
		return nil, ErrInvalidRememberToken
	}
	if user.RememberToken == "" || !tokenHashEquals(rawToken, user.RememberToken) {
		return nil, ErrInvalidRememberToken
	}
	return &user, nil
}

// ClearRememberToken invalidates user's remember-me token (if any) —
// call on logout so a stolen-but-not-yet-used cookie value stops working
// immediately.
func (g *Guard) ClearRememberToken(user *models.User) error {
	if err := g.db.Model(user).Update("remember_token", "").Error; err != nil {
		return err
	}
	user.RememberToken = ""
	return nil
}

// EncodeRememberCookie packs userID and a raw remember-me token into an
// encrypted string suitable for a cookie value, via encrypter —
// deliberately the persisted encryption.Encrypter (APP_KEY-backed), not
// Golite's own per-process cookie encryption (Context.SetCookie's
// Kernel.appKey). A remember-me cookie's entire purpose is surviving a
// browser restart; that only matters if it also survives a *server*
// restart, which is exactly what Kernel.appKey deliberately doesn't
// guarantee (see docs/architecture.md).
func EncodeRememberCookie(encrypter *encryption.Encrypter, userID uint, token string) (string, error) {
	return encrypter.EncryptString(fmt.Sprintf("%d|%s", userID, token))
}

// DecodeRememberCookie reverses EncodeRememberCookie.
func DecodeRememberCookie(encrypter *encryption.Encrypter, cookieValue string) (userID uint, token string, err error) {
	plaintext, err := encrypter.DecryptString(cookieValue)
	if err != nil {
		return 0, "", err
	}

	idPart, tokenPart, ok := strings.Cut(plaintext, "|")
	if !ok || tokenPart == "" {
		return 0, "", ErrInvalidRememberToken
	}

	id, err := strconv.ParseUint(idPart, 10, 64)
	if err != nil {
		return 0, "", ErrInvalidRememberToken
	}
	return uint(id), tokenPart, nil
}
