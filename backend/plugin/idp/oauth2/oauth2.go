// Package oauth2 is the plugin for OAuth2 Identity Provider.
package oauth2

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/pkg/errors"
	"golang.org/x/oauth2"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/idp"
)

// IdentityProvider represents an OAuth2 Identity Provider.
type IdentityProvider struct {
	client *http.Client
	config *storepb.OAuth2IdentityProviderConfig
}

// NewIdentityProvider initializes a new OAuth2 Identity Provider with the given configuration.
func NewIdentityProvider(config *storepb.OAuth2IdentityProviderConfig) (*IdentityProvider, error) {
	if config == nil {
		return nil, errors.New("the oauth2 config is empty")
	}
	if config.FieldMapping == nil {
		return nil, errors.New(`the field "fieldMapping" is empty but required`)
	}
	for v, field := range map[string]string{
		config.ClientId:                "clientId",
		config.ClientSecret:            "clientSecret",
		config.TokenUrl:                "tokenUrl",
		config.UserInfoUrl:             "userInfoUrl",
		config.FieldMapping.Identifier: "fieldMapping.identifier",
	} {
		if v == "" {
			return nil, errors.Errorf(`the field "%s" is empty but required`, field)
		}
	}

	return &IdentityProvider{
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: config.SkipTlsVerify,
				},
			},
		},
		config: config,
	}, nil
}

// ExchangeToken returns the exchanged OAuth2 token using the given authorization code.
// codeVerifier carries the PKCE verifier when the client used one; it may be empty.
func (p *IdentityProvider) ExchangeToken(ctx context.Context, redirectURL, code, codeVerifier string) (string, error) {
	authStyle := oauth2.AuthStyleInParams
	if p.config.GetAuthStyle() == storepb.OAuth2AuthStyle_IN_HEADER {
		authStyle = oauth2.AuthStyleInHeader
	}
	conf := &oauth2.Config{
		ClientID:     p.config.ClientId,
		ClientSecret: p.config.ClientSecret,
		RedirectURL:  redirectURL,
		Scopes:       p.config.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:   p.config.AuthUrl,
			TokenURL:  p.config.TokenUrl,
			AuthStyle: authStyle,
		},
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.client)
	var opts []oauth2.AuthCodeOption
	if codeVerifier != "" {
		opts = append(opts, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	}
	token, err := conf.Exchange(ctx, code, opts...)
	if err != nil {
		// The authorization code is a credential; never log it.
		slog.Error("Failed to exchange access token", log.WithError(err))
		return "", errors.Wrap(err, "failed to exchange access token")
	}

	accessToken, ok := token.Extra("access_token").(string)
	if !ok {
		slog.Error(`Missing "access_token" from authorization response`)
		return "", errors.New(`missing "access_token" from authorization response`)
	}

	return accessToken, nil
}

// UserInfo returns the parsed user information using the given OAuth2 token.
func (p *IdentityProvider) UserInfo(token string) (*storepb.IdentityProviderUserInfo, map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, p.config.UserInfoUrl, nil)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to new http request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	resp, err := p.client.Do(req)
	if err != nil {
		slog.Error("Failed to get user information", log.WithError(err))
		return nil, nil, errors.Wrap(err, "failed to get user information")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read response body", log.WithError(err))
		return nil, nil, errors.Wrap(err, "failed to read response body")
	}

	var claims map[string]any
	if err := json.Unmarshal(body, &claims); err != nil {
		// The body may carry personal data and tokens, so log its length only.
		slog.Error("Failed to unmarshal response body", slog.Int("bodyBytes", len(body)), log.WithError(err))
		return nil, nil, errors.Wrap(err, "failed to unmarshal response body")
	}

	userInfo := &storepb.IdentityProviderUserInfo{}
	if v, ok := idp.GetValueWithKey(claims, p.config.FieldMapping.Identifier).(string); ok {
		userInfo.Identifier = v
	}
	if p.config.FieldMapping.DisplayName != "" {
		if v, ok := idp.GetValueWithKey(claims, p.config.FieldMapping.DisplayName).(string); ok {
			userInfo.DisplayName = v
		}
	}
	if userInfo.DisplayName == "" {
		userInfo.DisplayName = userInfo.Identifier
	}
	if p.config.FieldMapping.Phone != "" {
		if v, ok := idp.GetValueWithKey(claims, p.config.FieldMapping.Phone).(string); ok {
			// Only set phone if it's valid.
			if err := common.ValidatePhone(v); err == nil {
				userInfo.Phone = v
			}
		}
	}
	return userInfo, claims, nil
}
