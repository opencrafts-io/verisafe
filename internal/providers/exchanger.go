package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/opencrafts-io/verisafe/internal/config"
	"golang.org/x/oauth2"
)

// Errors a TokenExchanger classifies. The distinction between ErrInvalidGrant
// and ErrProviderUnavailable is the important one: the first means the grant
// is genuinely dead and should be marked revoked, the second is transient and
// must leave the grant untouched. Conflating them would revoke every user's
// grant during a provider outage.
var (
	// ErrInvalidGrant means the provider rejected the refresh token itself:
	// the user revoked access, changed their password, or the token expired
	// after a long idle period. Re-authorization is the only fix.
	ErrInvalidGrant = errors.New("provider rejected the refresh token")

	// ErrProviderUnavailable means the provider failed transiently — a 5xx, a
	// timeout, a DNS failure. Retry later; change nothing.
	ErrProviderUnavailable = errors.New("provider is temporarily unavailable")

	// ErrRefreshUnsupported means this provider cannot refresh tokens.
	ErrRefreshUnsupported = errors.New("provider does not support token refresh")
)

// Token is a provider token response, reduced to what Verisafe stores.
type Token struct {
	AccessToken  string
	RefreshToken string // empty when the provider returned none
	IDToken      string
	ExpiresAt    time.Time
	// Scopes is what the provider says was granted, normalized. nil when the
	// provider does not report a scope field — which is different from an
	// empty grant and must not be treated as "nothing granted".
	Scopes []string
}

// TokenExchanger talks to a provider's token endpoint.
//
// It exists as an interface so the grant service — where all the interesting
// logic lives — can be tested against a hand-written fake with no network and
// no httptest juggling.
type TokenExchanger interface {
	// Exchange trades an authorization code for tokens.
	Exchange(
		ctx context.Context,
		d Descriptor,
		code, redirectURI, codeVerifier string,
	) (*Token, error)

	// Refresh trades a refresh token for a new access token.
	Refresh(ctx context.Context, d Descriptor, refreshToken string) (*Token, error)
}

type oauth2Exchanger struct {
	cfg    *config.Config
	client *http.Client
}

// NewOAuth2Exchanger returns the production TokenExchanger. Pass nil for
// httpClient to get one with a sane timeout.
func NewOAuth2Exchanger(cfg *config.Config, httpClient *http.Client) TokenExchanger {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &oauth2Exchanger{cfg: cfg, client: httpClient}
}

func (e *oauth2Exchanger) Exchange(
	ctx context.Context,
	d Descriptor,
	code, redirectURI, codeVerifier string,
) (*Token, error) {
	conf, err := d.OAuthConfig(e.cfg, redirectURI, nil)
	if err != nil {
		return nil, err
	}

	var opts []oauth2.AuthCodeOption
	if codeVerifier != "" {
		opts = append(opts, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	}

	tok, err := conf.Exchange(e.withClient(ctx), code, opts...)
	if err != nil {
		return nil, classify(err)
	}
	return toToken(d, tok), nil
}

func (e *oauth2Exchanger) Refresh(
	ctx context.Context,
	d Descriptor,
	refreshToken string,
) (*Token, error) {
	if !d.SupportsRefresh {
		return nil, ErrRefreshUnsupported
	}
	if refreshToken == "" {
		return nil, fmt.Errorf("%w: no refresh token stored", ErrInvalidGrant)
	}

	conf, err := d.OAuthConfig(e.cfg, "", nil)
	if err != nil {
		return nil, err
	}

	// TokenSource performs the refresh on first Token() call.
	src := conf.TokenSource(
		e.withClient(ctx),
		&oauth2.Token{RefreshToken: refreshToken},
	)
	tok, err := src.Token()
	if err != nil {
		return nil, classify(err)
	}

	out := toToken(d, tok)
	// Providers that do not rotate refresh tokens omit the field. Carrying the
	// one we just used forward keeps the caller from having to special-case it.
	if out.RefreshToken == "" {
		out.RefreshToken = refreshToken
	}
	return out, nil
}

func (e *oauth2Exchanger) withClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, e.client)
}

// toToken maps an oauth2.Token onto our reduced form, lifting the scope and
// id_token extras that oauth2 keeps in an untyped bag.
func toToken(d Descriptor, tok *oauth2.Token) *Token {
	out := &Token{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.Expiry,
	}

	if raw, ok := tok.Extra("id_token").(string); ok {
		out.IDToken = raw
	}

	// Only trust the scope field from providers that actually send one.
	// Leaving Scopes nil for the others is meaningful: it means "unknown",
	// not "none", and the grant service treats those differently.
	if d.ReportsScope {
		if raw, ok := tok.Extra("scope").(string); ok && raw != "" {
			out.Scopes = d.ParseScopeString(raw)
		}
	}

	return out
}

// classify maps a provider error onto one of the sentinels above.
func classify(err error) error {
	if err == nil {
		return nil
	}

	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) {
		switch {
		case retrieve.ErrorCode == "invalid_grant":
			return fmt.Errorf("%w: %s", ErrInvalidGrant, retrieve.ErrorCode)
		case retrieve.Response != nil && retrieve.Response.StatusCode >= 500:
			return fmt.Errorf(
				"%w: provider returned %d",
				ErrProviderUnavailable,
				retrieve.Response.StatusCode,
			)
		case retrieve.Response != nil && retrieve.Response.StatusCode == http.StatusTooManyRequests:
			return fmt.Errorf("%w: provider rate limited the request", ErrProviderUnavailable)
		}
		return fmt.Errorf("provider rejected the request: %w", err)
	}

	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}

	return err
}
