// Package auth is Golite's equivalent of Illuminate\Auth: session-based
// authentication (Guard), remember-me persistence (remember.go), and
// password reset (password_reset.go) — all built on packages this
// project already has (hashing, encryption, the session engine, GORM)
// rather than introducing a separate credential store. See
// docs/authentication.md.
package auth

import (
	"errors"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	gosession "Golite/app/Http/Session"
	models "Golite/app/Models"
	"Golite/hashing"
)

// sessionUserKey is the session key Login/Logout/Check/User store the
// authenticated user's ID under.
const sessionUserKey = "auth_user_id"

// Guard is Golite's session-based authentication engine — the equivalent
// of Laravel's Illuminate\Auth\SessionGuard. It holds no per-request
// state itself (every method takes whatever request-scoped
// *gosession.Session it needs to read or write), so one Guard — built
// once by AuthServiceProvider and bound into the container as "auth" —
// is shared safely across every concurrent request.
type Guard struct {
	db     *gorm.DB
	hasher hashing.Hasher
}

// NewGuard builds a Guard backed by db and hasher.
func NewGuard(db *gorm.DB, hasher hashing.Hasher) *Guard {
	return &Guard{db: db, hasher: hasher}
}

// Register creates a new user with a bcrypt-hashed password. It returns
// ErrEmailTaken if the email is already registered.
func (g *Guard) Register(name, email, password string) (*models.User, error) {
	user := models.User{
		Name:     name,
		Email:    email,
		Password: g.hasher.Make(password),
	}
	if err := g.db.Create(&user).Error; err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return &user, nil
}

// Attempt looks up email and checks password against its stored hash,
// returning ErrInvalidCredentials for either a nonexistent email or a
// wrong password — never anything that would let a caller tell the two
// apart.
func (g *Guard) Attempt(email, password string) (*models.User, error) {
	var user models.User
	if err := g.db.Where("email = ?", email).First(&user).Error; err != nil {
		return nil, ErrInvalidCredentials
	}
	if !g.hasher.Check(password, user.Password) {
		return nil, ErrInvalidCredentials
	}
	return &user, nil
}

// Login stores user's ID in sess. Callers should call
// Context.RegenerateSession right afterward to prevent session fixation
// around this privilege change — see AuthController.Register/Login.
func (g *Guard) Login(sess *gosession.Session, user *models.User) {
	sess.Put(sessionUserKey, user.ID)
}

// Logout removes the authenticated user from sess. Callers that also
// issued a remember-me token should call ClearRememberToken first (see
// AuthController.Logout) and invalidate the session afterward (see
// Context.InvalidateSession) — Logout on its own only clears this one
// key, matching Login's narrow scope.
func (g *Guard) Logout(sess *gosession.Session) {
	sess.Forget(sessionUserKey)
}

// Check reports whether sess has an authenticated user.
func (g *Guard) Check(sess *gosession.Session) bool {
	return sess.Has(sessionUserKey)
}

// User resolves the currently authenticated user from sess, or nil if
// none (either never logged in, or Logout/Invalidate cleared it).
//
// sess.Get's return type depends on how the session round-tripped to get
// here: within the same request Login set it in (before any save/reload),
// it's still a plain Go uint; on every later request, it's been through
// a JSON encode/decode cycle in the session driver (see
// app/Http/Session/Session.go's encode/decodeSession) and comes back as
// float64, JSON's only number type. Both are handled below.
func (g *Guard) User(sess *gosession.Session) *models.User {
	raw := sess.Get(sessionUserKey)
	if raw == nil {
		return nil
	}

	var id uint
	switch v := raw.(type) {
	case uint:
		id = v
	case int:
		id = uint(v)
	case int64:
		id = uint(v)
	case float64:
		id = uint(v)
	default:
		return nil
	}

	var user models.User
	if err := g.db.First(&user, id).Error; err != nil {
		return nil
	}
	return &user
}

// isDuplicateKeyError reports whether err is MySQL error 1062 (Duplicate
// entry) — what a unique-index violation surfaces as.
func isDuplicateKeyError(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
