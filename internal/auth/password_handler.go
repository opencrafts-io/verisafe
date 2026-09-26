package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	devicesvc "github.com/opencrafts-io/verisafe/internal/service/device"
	"github.com/opencrafts-io/verisafe/internal/tokens"
)

const (
	passwordLoginWindow        = 15 * time.Minute
	passwordLoginIPLimit       = 100
	passwordLoginIdentityLimit = 10
	passwordLoginRatePrefix    = "password_login_limit:"
)

type setPasswordRequest struct {
	Password string `json:"password" validate:"required,min=12,max=128"`
}

type passwordLoginRequest struct {
	Email       string `json:"email"        validate:"required"`
	Password    string `json:"password"     validate:"required,min=12,max=128"`
	DeviceName  string `json:"device_name"`
	DeviceToken string `json:"device_token"`
}

// SetPasswordHandler godoc
//
// @Summary      Add or change the authenticated account's password
// @Description  Adds a password credential to the signed-in human account or replaces its existing credential. The caller must already have a user access token; password recovery is not provided.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body  auth.setPasswordRequest  true  "New password"
// @Success      204  "Password saved"
// @Failure      400  {object}  core.APIError  "Password does not meet requirements"
// @Failure      401  {object}  core.APIError  "A user access token is required"
// @Failure      403  {object}  core.APIError  "Only active human accounts can use passwords"
// @Security     BearerToken
// @Router       /auth/password [put]
// SetPasswordHandler adds or replaces the authenticated account's password.
// It does not provide password recovery: callers must already hold a Verisafe
// user session.
func (h *AuthHandler) SetPasswordHandler(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || claims == nil || middleware.IsServiceToken(r.Context()) {
		return core.Public(
			core.ErrUnauthorized,
			"a user access token is required",
		)
	}
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return core.Public(core.ErrUnauthorized, "invalid account identity")
	}

	var req setPasswordRequest
	if err := decodePasswordJSON(
		w,
		r,
		&req,
	); err != nil ||
		!validPasswordLength(req.Password) {
		return core.Public(
			core.ErrInvalidInput,
			fmt.Sprintf(
				"password must be between %d and %d characters",
				passwordMinRunes,
				passwordMaxRunes,
			),
		)
	}

	passwordHash, err := hashPassword(req.Password)
	if err != nil {
		h.logger.Error(
			"failed to hash account password",
			slog.Any("error", err),
		)
		return core.ErrInternal
	}

	err = core.InTxDo(r.Context(), h.db, func(tx pgx.Tx) error {
		repo := repository.New(tx)
		account, err := repo.GetAccountByID(r.Context(), accountID)
		if err != nil {
			return err
		}
		if account.Type != repository.AccountTypeHuman ||
			account.DeletedAt != nil {
			return core.ErrForbidden
		}
		return repo.SetAccountPassword(
			r.Context(),
			repository.SetAccountPasswordParams{
				AccountID:    accountID,
				PasswordHash: passwordHash,
			},
		)
	})
	if err != nil {
		h.logger.Error(
			"failed to save account password",
			slog.Any("error", err),
		)
		if errors.Is(err, core.ErrForbidden) {
			return err
		}
		return core.ErrInternal
	}

	h.logger.Info(
		"account password set",
		slog.String("account_id", accountID.String()),
	)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// PasswordLoginHandler godoc
//
// @Summary      Sign in with an account email and password
// @Description  Authenticates an existing human account that has previously set a password, then returns the usual Verisafe access and refresh token pair. This endpoint does not create accounts.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      auth.passwordLoginRequest  true  "Email, password, and optional device details"
// @Success      200  {object}  auth.tokenResponse
// @Failure      400  {object}  core.APIError  "Malformed login request"
// @Failure      401  {object}  core.APIError  "Invalid email or password"
// @Failure      429  {object}  core.APIError  "Too many login attempts"
// @Failure      503  {object}  core.APIError  "Login rate limiter unavailable"
// @Router       /auth/password/login [post]
// PasswordLoginHandler authenticates an existing human account by email and
// password, registers the device, and issues the standard Verisafe token pair.
// It never creates an account.
func (h *AuthHandler) PasswordLoginHandler(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req passwordLoginRequest
	if err := decodePasswordJSON(w, r, &req); err != nil {
		return core.Public(core.ErrInvalidInput, "invalid login request")
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if len(req.Email) == 0 || len(req.Email) > 255 ||
		!validPasswordLength(req.Password) {
		return core.Public(core.ErrInvalidInput, "invalid login request")
	}

	clientIP, err := middleware.ClientIP(
		r,
		h.auth.config.TrustedProxyPrefixes(),
	)
	if err != nil {
		return core.Public(core.ErrInvalidInput, "invalid client address")
	}
	if err := h.checkPasswordLoginRateLimit(
		r,
		clientIP,
		req.Email,
	); err != nil {
		if errors.Is(err, errPasswordLoginRateLimited) {
			core.WriteError(
				w,
				http.StatusTooManyRequests,
				"too many login attempts; try again later",
			)
			return nil
		}
		h.logger.Error(
			"password login rate limiter unavailable",
			slog.Any("error", err),
		)
		return core.ErrUnavailable
	}
	var country *string
	if h.geoLocator != nil {
		if info, err := h.geoLocator.Lookup(clientIP); err != nil {
			h.logger.Warn(
				"geo lookup failed during password login",
				slog.Any("error", err),
			)
		} else {
			country = &info.Country.ISOCode
		}
	}

	conn, err := h.db.Acquire(r.Context())
	if err != nil {
		return core.ErrInternal
	}
	credential, lookupErr := repository.New(conn).
		GetPasswordCredentialByEmail(r.Context(), req.Email)
	conn.Release()
	if lookupErr != nil {
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			burnPasswordHash(req.Password)
			return invalidPasswordLogin()
		}
		h.logger.Error(
			"failed to find password credential",
			slog.Any("error", lookupErr),
		)
		return core.ErrInternal
	}
	if !verifyPassword(req.Password, credential.PasswordHash) {
		return invalidPasswordLogin()
	}

	var pair *tokens.TokenPair
	err = core.InTxDo(r.Context(), h.db, func(tx pgx.Tx) error {
		repo := repository.New(tx)
		account, err := repo.GetAccountByID(r.Context(), credential.ID)
		if err != nil {
			return err
		}
		if account.Type != repository.AccountTypeHuman ||
			account.DeletedAt != nil {
			return pgx.ErrNoRows
		}

		device, err := devicesvc.NewDeviceService(repo).RegisterDevice(
			r.Context(),
			devicesvc.DeviceRegistrationInput{
				UserID:      account.ID,
				DeviceName:  req.DeviceName,
				Platform:    authPlatformMobileValue,
				DeviceToken: req.DeviceToken,
				IpAddress:   &clientIP,
				Country:     country,
			},
		)
		if err != nil {
			return err
		}

		pair, err = tokens.NewTokenService(repo, h.cacher, h.auth.config).
			IssueTokenPair(r.Context(), account.ID, device.ID, uuid.New())
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return invalidPasswordLogin()
		}
		h.logger.Error(
			"password login transaction failed",
			slog.Any("error", err),
		)
		return core.ErrInternal
	}

	h.clearPasswordLoginRateLimit(r, clientIP, req.Email)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(tokenResponse{
		AccessToken:      pair.AccessToken,
		RefreshToken:     pair.RawRefreshToken,
		AccessExpiresAt:  pair.AccessExpiresAt,
		RefreshExpiresAt: pair.RefreshExpiresAt,
	})
}

var errPasswordLoginRateLimited = errors.New("password login rate limited")

func (h *AuthHandler) checkPasswordLoginRateLimit(
	r *http.Request,
	clientIP netip.Addr,
	email string,
) error {
	ipHash := tokens.HashToken(clientIP.String())
	identityHash := tokens.HashToken(clientIP.String() + "\x00" + email)

	ipCount, err := h.cacher.IncrementWithTTL(
		r.Context(), passwordLoginRatePrefix+"ip:"+ipHash, passwordLoginWindow,
	)
	if err != nil {
		return err
	}
	identityCount, err := h.cacher.IncrementWithTTL(
		r.Context(),
		passwordLoginRatePrefix+"identity:"+identityHash,
		passwordLoginWindow,
	)
	if err != nil {
		return err
	}
	if ipCount > passwordLoginIPLimit ||
		identityCount > passwordLoginIdentityLimit {
		return errPasswordLoginRateLimited
	}
	return nil
}

func (h *AuthHandler) clearPasswordLoginRateLimit(
	r *http.Request,
	clientIP netip.Addr,
	email string,
) {
	ipHash := tokens.HashToken(clientIP.String())
	identityHash := tokens.HashToken(clientIP.String() + "\x00" + email)
	for _, key := range []string{
		passwordLoginRatePrefix + "ip:" + ipHash,
		passwordLoginRatePrefix + "identity:" + identityHash,
	} {
		if err := h.cacher.Delete(r.Context(), key); err != nil {
			h.logger.Warn(
				"failed to clear password login rate limit",
				slog.Any("error", err),
			)
		}
	}
}

func invalidPasswordLogin() error {
	return core.Public(core.ErrUnauthorized, "invalid email or password")
}

func decodePasswordJSON(
	w http.ResponseWriter,
	r *http.Request,
	dest any,
) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("request body contains multiple JSON values")
	}
	return nil
}
