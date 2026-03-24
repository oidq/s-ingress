package oidc

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"codeberg.org/oidq/s-ingress/pkg/proxy"
	"golang.org/x/oauth2"
)

type authInfo struct {
	Subject    string   `json:"sub"`
	Email      string   `json:"email"`
	Groups     []string `json:"grp"`
	ClientName string   `json:"clt"`

	// AuthenticatedAt is a UNIX timestamp of the last successful authentication.
	AuthenticatedAt int64 `json:"ath"`

	TokenExpiry  int64  `json:"tkn_exp"`
	AccessToken  string `json:"tkn_acc"`
	RefreshToken string `json:"tkn_rfr"`
}

func (ai *authInfo) marshal() []byte {
	data, err := json.Marshal(ai)
	if err != nil {
		panic("failed to marshal authInfo: " + err.Error())
	}

	return data
}

func unmarshalAuthInfo(data []byte) (*authInfo, error) {
	var ai authInfo
	err := json.Unmarshal(data, &ai)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal authInfo: %w", err)
	}

	return &ai, nil
}

const renewOffset = 5 * time.Minute

func handleOidcCallback(rCtx *proxy.RequestContext, client *clientConfig) (*authInfo, error) {
	state, err := rCtx.R.Cookie(cookieState)
	if err != nil {
		return nil, rCtx.BadRequest("state cookie not found")
	}
	if rCtx.R.URL.Query().Get("state") != state.Value {
		return nil, rCtx.BadRequest("state not found")
	}

	oauth2Token, err := client.oauth2Config.Exchange(rCtx, rCtx.R.URL.Query().Get("code"))
	if err != nil {
		return nil, fmt.Errorf("could not exchange code for token: %w", err)
	}

	return oauthToAuthInfo(rCtx, client, oauth2Token, time.Now(), true)
}

func handleRenewingAuthInfo(rCtx *proxy.RequestContext, client *clientConfig, info *authInfo) *authInfo {
	if info.RefreshToken == "" {
		return info // we do not have a refresh token
	}

	tokenExpiration := time.Unix(info.TokenExpiry, 0)
	if tokenExpiration.After(time.Now().Add(renewOffset)) {
		return info // token is still valid enough
	}

	newAuth, err := renewAuthInfo(rCtx, client, info)
	if err != nil {
		rCtx.Log.Warn("failed to renew token", slog.String("error", err.Error()))
		return info
	}

	return newAuth
}

func renewAuthInfo(rCtx *proxy.RequestContext, client *clientConfig, info *authInfo) (*authInfo, error) {
	token := &oauth2.Token{
		AccessToken:  info.AccessToken,
		RefreshToken: info.RefreshToken,
		Expiry:       time.Unix(info.TokenExpiry, 0),
	}
	tokenSource := client.oauth2Config.TokenSource(rCtx, token)
	expiryTokenSource := oauth2.ReuseTokenSourceWithExpiry(token, tokenSource, renewOffset)

	newToken, err := expiryTokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to renew token: %w", err)
	}

	newAuthInfo, err := oauthToAuthInfo(rCtx, client, newToken, time.Unix(info.AuthenticatedAt, 0), false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert renewed token: %w", err)
	}

	cookie, err := generateJwtCookie(rCtx, client, newAuthInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to generate jwt cookie: %w", err)
	}
	rCtx.SetResponseCookie(cookie)

	rCtx.Log.Info("token renewed")

	return newAuthInfo, nil
}

func oauthToAuthInfo(rCtx *proxy.RequestContext, client *clientConfig, token *oauth2.Token, authAt time.Time, verifyNonce bool) (*authInfo, error) {
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token field found in oauth2 token")
	}
	idToken, err := client.idTokenVerifier.Verify(rCtx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("could not verify id token: %w", err)
	}

	if verifyNonce {
		nonce, err := rCtx.R.Cookie(cookieNonce)
		if err != nil {
			return nil, rCtx.BadRequest("nonce cookie not found")
		}
		if idToken.Nonce != nonce.Value {
			return nil, rCtx.BadRequest("nonce cookie did not match")
		}
	}

	var c claims
	err = idToken.Claims(&c)
	if err != nil {
		return nil, fmt.Errorf("could not parse id token claims: %w", err)
	}

	return &authInfo{
		Subject:    idToken.Subject,
		Email:      c.Email,
		Groups:     c.Groups,
		ClientName: client.name,

		RefreshToken: token.RefreshToken,
		TokenExpiry:  token.Expiry.Unix(),
		AccessToken:  token.AccessToken,

		AuthenticatedAt: authAt.Unix(),
	}, nil
}
