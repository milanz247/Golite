package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/encryption"
)

// CryptoController demonstrates the encryption package (see
// docs/encryption.md): AES-256-GCM encrypt/decrypt via a method-injected
// *encryption.Encrypter, resolved automatically by apphttp.Inject (see
// routes/web.go) — Golite's equivalent of a Laravel controller
// type-hinting Illuminate\Encryption\Encrypter (the Crypt facade)
// straight on the action method.
type CryptoController struct {
	Controller
}

// NewCryptoController creates a new CryptoController.
func NewCryptoController() *CryptoController {
	return &CryptoController{}
}

// Encrypt handles GET /crypto/encrypt?value=...
func (cc *CryptoController) Encrypt(c *apphttp.Context, encrypter *encryption.Encrypter) {
	value, _ := c.Input("value", "a secret message").(string)

	payload, err := encrypter.EncryptString(value)
	if err != nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to encrypt"})
		return
	}
	c.JSON(http.StatusOK, map[string]string{"payload": payload})
}

// Decrypt handles GET /crypto/decrypt?payload=...
func (cc *CryptoController) Decrypt(c *apphttp.Context, encrypter *encryption.Encrypter) {
	payload, _ := c.Input("payload", "").(string)
	if payload == "" {
		c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "payload query param is required (see /crypto/encrypt)"})
		return
	}

	value, err := encrypter.DecryptString(payload)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "invalid or tampered payload"})
		return
	}
	c.JSON(http.StatusOK, map[string]string{"value": value})
}
