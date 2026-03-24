package oidc

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"

	"codeberg.org/oidq/s-ingress/pkg/proxy"
	"github.com/go-jose/go-jose/v4"
)

var errCookieNotFound = fmt.Errorf("cookie not found")
var errCookieInvalid = fmt.Errorf("invalid cookie")

func newEncrypter(key []byte) (jose.Encrypter, error) {
	enc, err := jose.NewEncrypter(
		jose.A256GCM,
		jose.Recipient{Algorithm: jose.A256GCMKW, Key: key},
		&jose.EncrypterOptions{
			Compression: jose.DEFLATE,
		},
	)
	if err != nil {
		return nil, err
	}

	return enc, nil
}

func decrypt(key []byte, encryptedData string) ([]byte, error) {
	encrypted, err := jose.ParseEncrypted(
		encryptedData,
		[]jose.KeyAlgorithm{jose.A256GCMKW},
		[]jose.ContentEncryption{jose.A256GCM},
	)
	if err != nil {
		return nil, fmt.Errorf("parse encrypted: %w", err)
	}
	data, err := encrypted.Decrypt(key)
	if err != nil {
		return nil, fmt.Errorf("decrypt encrypted data: %w", err)
	}

	return data, nil
}

func generateJwtCookie(rCtx *proxy.RequestContext, client *clientConfig, info *authInfo) (*http.Cookie, error) {
	raw := info.marshal()

	encrypted, err := client.encryptor.Encrypt(raw)
	if err != nil {
		return nil, fmt.Errorf("encrypt info: %w", err)
	}

	serialized, err := encrypted.CompactSerialize()
	if err != nil {
		return nil, fmt.Errorf("serialize info: %w", err)
	}

	return &http.Cookie{
		Name:     getCookieName(client),
		Value:    serialized,
		MaxAge:   int(client.expiration.Milliseconds() / 1000),
		Secure:   rCtx.R.TLS != nil,
		HttpOnly: true,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Domain:   client.cookieDomain,
	}, nil
}

func extractAuthInfo(rCtx *proxy.RequestContext, client *clientConfig) (*authInfo, error) {
	c, err := rCtx.R.Cookie(getCookieName(client))
	switch {
	case errors.Is(err, http.ErrNoCookie):
		return nil, errCookieNotFound
	case err != nil:
		return nil, fmt.Errorf("get cookie %s: %w", getCookieName(client), err)
	}

	data, err := decrypt(client.encryptionKey, c.Value)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errCookieInvalid, err)
	}

	info, err := unmarshalAuthInfo(data)
	if err != nil {
		return nil, fmt.Errorf("unmarshal info: %w", err)
	}

	return info, nil
}

func getCookieName(client *clientConfig) string {
	return "_oidq_" + client.name
}

func extractJwtAesKeySecret(value []byte) []byte {
	hash := sha256.Sum256(value)
	return hash[:]
}
