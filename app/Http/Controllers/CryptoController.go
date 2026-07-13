package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/encryption"
)

// CryptoController demonstrates the encryption package (see
// docs/encryption.md): AES-256-GCM encrypt/decrypt via a
// constructor-injected *encryption.Encrypter — Golite's equivalent of a
// Laravel controller that type-hints Illuminate\Encryption\Encrypter (the
// Crypt facade) in its constructor.
type CryptoController struct {
	Controller
	encrypter *encryption.Encrypter
}

// NewCryptoController constructs a CryptoController with an injected
// *encryption.Encrypter, resolved from the container's "encrypter"
// binding in routes/web.go.
func NewCryptoController(encrypter *encryption.Encrypter) *CryptoController {
	return &CryptoController{encrypter: encrypter}
}

// Encrypt handles GET /crypto/encrypt?value=...
func (cc *CryptoController) Encrypt(c *apphttp.Context) {
	value, _ := c.Input("value", "a secret message").(string)

	payload, err := cc.encrypter.EncryptString(value)
	if err != nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to encrypt"})
		return
	}
	c.JSON(http.StatusOK, map[string]string{"payload": payload})
}

// Decrypt handles GET /crypto/decrypt?payload=...
func (cc *CryptoController) Decrypt(c *apphttp.Context) {
	payload, _ := c.Input("payload", "").(string)
	if payload == "" {
		c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "payload query param is required (see /crypto/encrypt)"})
		return
	}

	value, err := cc.encrypter.DecryptString(payload)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "invalid or tampered payload"})
		return
	}
	c.JSON(http.StatusOK, map[string]string{"value": value})
}
