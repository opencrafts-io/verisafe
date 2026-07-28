package servicetoken_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers/servicetoken"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	servicetokensvc "github.com/opencrafts-io/verisafe/internal/service/servicetoken"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
	"github.com/opencrafts-io/verisafe/internal/tokens"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubService lets each test override only the methods it exercises.
type stubService struct {
	servicetokensvc.Service
	verifyBotAccount func(ctx context.Context, id uuid.UUID) (repository.Account, error)
	getByID          func(ctx context.Context, id uuid.UUID) (repository.ServiceToken, error)
	update           func(ctx context.Context, in repository.UpdateServiceTokenParams) error
	rotate           func(ctx context.Context, in repository.RotateServiceTokenParams) error
}

func (s stubService) VerifyBotAccount(
	ctx context.Context, id uuid.UUID,
) (repository.Account, error) {
	return s.verifyBotAccount(ctx, id)
}

func (s stubService) GetByID(
	ctx context.Context, id uuid.UUID,
) (repository.ServiceToken, error) {
	return s.getByID(ctx, id)
}

func (s stubService) Update(
	ctx context.Context, in repository.UpdateServiceTokenParams,
) error {
	return s.update(ctx, in)
}

func (s stubService) Rotate(
	ctx context.Context, in repository.RotateServiceTokenParams,
) error {
	return s.rotate(ctx, in)
}

// This was the only handler whose error bodies were plain text via
// http.Error, not JSON-shaped strings under a text/plain header like every
// other migrated handler. Per an explicit decision, it now goes through
// core.WriteError like the rest of the API: this test asserts the resulting
// {"error":"..."} body under application/json, a real body-shape change
// recorded in ADR 0009, not the header-only change other handlers got.
func TestListAllServiceTokens_ConnectionAcquisitionFailure(t *testing.T) {
	h := &servicetoken.ServiceTokenHandler{
		Logger: discardLogger(),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	testsupport.WireCase{
		Handler: core.AppHandler(h.ListAllServiceTokens),
		Request: httptest.NewRequest(
			"GET", "/api/v1/admin/service-tokens", nil,
		),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"Internal server error"}` + "\n",
	}.Run(t)
}

func TestCreateServiceToken_MissingClaimsIsUnauthorized(t *testing.T) {
	h := &servicetoken.ServiceTokenHandler{Logger: discardLogger()}

	testsupport.WireCase{
		Handler: core.AppHandler(h.CreateServiceToken),
		Request: httptest.NewRequest(
			"POST", "/api/v1/service-tokens", strings.NewReader("{}"),
		),
		WantStatus:      401,
		WantContentType: "application/json",
		WantBody:        `{"error":"unauthorized: missing claims"}` + "\n",
	}.Run(t)
}

func TestCreateServiceToken_NonBotAccountIsForbidden(t *testing.T) {
	subject := uuid.New()
	h := &servicetoken.ServiceTokenHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, testsupport.NewTx(t)),
		Service: func(repository.Querier) servicetokensvc.Service {
			return stubService{
				verifyBotAccount: func(context.Context, uuid.UUID) (repository.Account, error) {
					return repository.Account{}, servicetokensvc.ErrNotBotAccount
				},
			}
		},
	}

	req := httptest.NewRequest(
		"POST", "/api/v1/service-tokens", strings.NewReader(`{"name":"x"}`),
	)
	req = req.WithContext(middleware.WithClaims(req.Context(), &tokens.VerisafeClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject.String()},
	}))

	testsupport.WireCase{
		Handler:         core.AppHandler(h.CreateServiceToken),
		Request:         req,
		WantStatus:      403,
		WantContentType: "application/json",
		WantBody:        `{"error":"Only bot accounts can create service tokens"}` + "\n",
	}.Run(t)
}

func TestCreateServiceToken_AccountLookupFailureIsNotFound(t *testing.T) {
	subject := uuid.New()
	h := &servicetoken.ServiceTokenHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, testsupport.NewTx(t)),
		Service: func(repository.Querier) servicetokensvc.Service {
			return stubService{
				verifyBotAccount: func(context.Context, uuid.UUID) (repository.Account, error) {
					return repository.Account{}, errors.New("connection reset")
				},
			}
		},
	}

	req := httptest.NewRequest(
		"POST", "/api/v1/service-tokens", strings.NewReader(`{"name":"x"}`),
	)
	req = req.WithContext(middleware.WithClaims(req.Context(), &tokens.VerisafeClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject.String()},
	}))

	testsupport.WireCase{
		Handler:         core.AppHandler(h.CreateServiceToken),
		Request:         req,
		WantStatus:      404,
		WantContentType: "application/json",
		WantBody:        `{"error":"Account not found"}` + "\n",
	}.Run(t)
}

func TestGetServiceToken_InvalidTokenIDIsRejectedBeforeAnyDatabaseWork(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/service-tokens/not-a-uuid", nil)

	testsupport.WireCase{
		Handler:         core.AppHandler((&servicetoken.ServiceTokenHandler{Logger: discardLogger()}).GetServiceToken),
		Request:         req,
		Authenticated:   true,
		WantStatus:      400,
		WantContentType: "application/json",
		WantBody:        `{"error":"Invalid token ID"}` + "\n",
	}.Run(t)
}

// GetServiceToken treats ANY error from the lookup as "not found", matching
// what this endpoint did before the extraction (no errors.Is check existed).
func TestGetServiceToken_AnyLookupErrorIsNotFound(t *testing.T) {
	subject := uuid.New()
	tokenID := uuid.New()
	// Acquire must succeed for this test since GetServiceToken has no Begin
	// step at all -- it queries the acquired connection directly.
	h := &servicetoken.ServiceTokenHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, testsupport.NewTx(t)),
		Service: func(repository.Querier) servicetokensvc.Service {
			return stubService{
				getByID: func(context.Context, uuid.UUID) (repository.ServiceToken, error) {
					return repository.ServiceToken{}, errors.New("driver hiccup")
				},
			}
		},
	}

	req := httptest.NewRequest(
		"GET", "/api/v1/service-tokens/"+tokenID.String(), nil,
	)
	req = req.WithContext(middleware.WithClaims(req.Context(), &tokens.VerisafeClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject.String()},
	}))

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetServiceToken),
		Request:         req,
		WantStatus:      404,
		WantContentType: "application/json",
		WantBody:        `{"error":"Service token not found"}` + "\n",
	}.Run(t)
}

func TestGetServiceToken_NonOwnerNonAdminIsAccessDenied(t *testing.T) {
	subject := uuid.New()
	tokenOwner := uuid.New()
	tokenID := uuid.New()

	h := &servicetoken.ServiceTokenHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, testsupport.NewTx(t)),
		Service: func(repository.Querier) servicetokensvc.Service {
			return stubService{
				getByID: func(context.Context, uuid.UUID) (repository.ServiceToken, error) {
					return repository.ServiceToken{ID: tokenID, AccountID: tokenOwner}, nil
				},
			}
		},
	}

	req := httptest.NewRequest(
		"GET", "/api/v1/service-tokens/"+tokenID.String(), nil,
	)
	req = req.WithContext(middleware.WithClaims(req.Context(), &tokens.VerisafeClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject.String()},
	}))
	req = req.WithContext(middleware.WithPermissions(req.Context(), nil))

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetServiceToken),
		Request:         req,
		WantStatus:      403,
		WantContentType: "application/json",
		WantBody:        `{"error":"Access denied"}` + "\n",
	}.Run(t)
}

// This is the concrete demonstration of the pre-existing bug fix decided
// during this migration: UpdateServiceToken previously re-fetched the
// updated row AFTER tx.Commit() on the same, by-then-closed transaction,
// which pgx would reject. The service now fetches before commit, so a
// successful update returns 200 with the token rather than a 500 despite
// having actually saved. The stub's GetByID is called twice (existence
// check, then post-update fetch) and both happen inside the one transaction
// the mock commits successfully.
func TestUpdateServiceToken_SuccessfulUpdateReturns200WithTheToken(t *testing.T) {
	subject := uuid.New()
	tokenID := uuid.New()
	updatedName := "renamed"

	tx := testsupport.NewTx(t)
	tx.EXPECT().Commit(gomock.Any()).Return(nil)

	getCalls := 0
	h := &servicetoken.ServiceTokenHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, tx),
		Service: func(repository.Querier) servicetokensvc.Service {
			return stubService{
				getByID: func(context.Context, uuid.UUID) (repository.ServiceToken, error) {
					getCalls++
					name := "original"
					if getCalls > 1 {
						name = updatedName
					}
					return repository.ServiceToken{
						ID: tokenID, AccountID: subject, Name: name,
						UseCount: intPtr(0),
					}, nil
				},
				update: func(context.Context, repository.UpdateServiceTokenParams) error {
					return nil
				},
			}
		},
	}

	req := httptest.NewRequest(
		"PUT", "/api/v1/service-tokens/"+tokenID.String(),
		strings.NewReader(`{"name":"`+updatedName+`"}`),
	)
	req = req.WithContext(middleware.WithClaims(req.Context(), &tokens.VerisafeClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject.String()},
	}))

	rr := httptest.NewRecorder()
	core.AppHandler(h.UpdateServiceToken).ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	assert.Contains(t, rr.Body.String(), updatedName)
	assert.Equal(t, 2, getCalls, "expected the existence check and the post-update fetch")
}

func intPtr(i int) *int32 { v := int32(i); return &v }
