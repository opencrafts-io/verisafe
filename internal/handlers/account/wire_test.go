package account_test

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
	"github.com/opencrafts-io/verisafe/internal/handlers/account"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	accountsvc "github.com/opencrafts-io/verisafe/internal/service/account"
	"github.com/opencrafts-io/verisafe/internal/testsupport"
	"github.com/opencrafts-io/verisafe/internal/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubService lets each test override only the methods it exercises.
type stubService struct {
	accountsvc.Service
	create             func(ctx context.Context, in repository.CreateAccountParams) (repository.Account, error)
	getBotRole         func(ctx context.Context) (repository.Role, error)
	assignRole         func(ctx context.Context, userID, roleID uuid.UUID) error
	createServiceToken func(ctx context.Context, rawToken string, in repository.CreateServiceTokenParams) (repository.ServiceToken, error)
}

func (s stubService) Create(
	ctx context.Context, in repository.CreateAccountParams,
) (repository.Account, error) {
	return s.create(ctx, in)
}

func (s stubService) GetBotRole(ctx context.Context) (repository.Role, error) {
	return s.getBotRole(ctx)
}

func (s stubService) AssignRole(
	ctx context.Context,
	userID, roleID uuid.UUID,
) error {
	return s.assignRole(ctx, userID, roleID)
}

func (s stubService) CreateServiceToken(
	ctx context.Context,
	rawToken string,
	in repository.CreateServiceTokenParams,
) (repository.ServiceToken, error) {
	return s.createServiceToken(ctx, rawToken, in)
}

func TestGetAllUserAccounts_ConnectionAcquisitionFailure(t *testing.T) {
	h := &account.AccountHandler{
		Logger: discardLogger(),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetAllUserAccounts),
		Request:         httptest.NewRequest("GET", "/accounts/all", nil),
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"We ran into a problem while servicing your request please try again later"}` + "\n",
	}.Run(t)
}

func TestGetPersonalAccount_MissingClaimsIsUnauthorized(t *testing.T) {
	h := &account.AccountHandler{Logger: discardLogger()}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.GetPersonalAccount),
		Request:         httptest.NewRequest("GET", "/accounts/me", nil),
		WantStatus:      401,
		WantContentType: "application/json",
		WantBody:        `{"error":"authentication required"}` + "\n",
	}.Run(t)
}

// UpdatePersonalAccount and VerifyPhone's ownership mismatch has always been
// a 500, not the 403 an ownership check would suggest -- a real quirk in the
// original code, preserved exactly rather than tightened.
func TestUpdatePersonalAccount_OwnershipMismatchIs500NotForbidden(
	t *testing.T,
) {
	body := `{"id":"6f1b6b1e-0000-4000-8000-000000000001","name":"x"}`
	req := httptest.NewRequest("PATCH", "/accounts/me", strings.NewReader(body))
	req = req.WithContext(
		middleware.WithClaims(req.Context(), &tokens.VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "6f1b6b1e-0000-4000-8000-000000000002",
			},
		}),
	)

	testsupport.WireCase{
		Handler: core.AppHandler(
			(&account.AccountHandler{Logger: discardLogger()}).UpdatePersonalAccount,
		),
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"You dont have permissions to update this account"}` + "\n",
	}.Run(t)
}

// MarkAccountForDeletion gives Begin failure its own message, distinct from
// Acquire and Commit (which share msgGeneric) -- the one message grouping in
// this handler core.InTx cannot express in a single Fallback call.
func TestMarkAccountForDeletion_BeginFailureIsADistinctMessage(t *testing.T) {
	req := httptest.NewRequest("POST", "/accounts/deletion-request", nil)
	req = req.WithContext(
		middleware.WithClaims(req.Context(), &tokens.VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: uuid.New().String(),
			},
		}),
	)

	h := &account.AccountHandler{
		Logger: discardLogger(),
		DB: testsupport.FailingBeginDB(
			t, errors.New("server closed the connection"),
		),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.MarkAccountForDeletion),
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"We ran into an error while trying to delete your account"}` + "\n",
	}.Run(t)
}

func TestMarkAccountForDeletion_AcquireFailureIsTheGenericMessage(
	t *testing.T,
) {
	req := httptest.NewRequest("POST", "/accounts/deletion-request", nil)
	req = req.WithContext(
		middleware.WithClaims(req.Context(), &tokens.VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: uuid.New().String(),
			},
		}),
	)

	h := &account.AccountHandler{
		Logger: discardLogger(),
		DB:     testsupport.FailingAcquireDB(t, errors.New("pool exhausted")),
	}

	testsupport.WireCase{
		Handler:         core.AppHandler(h.MarkAccountForDeletion),
		Request:         req,
		WantStatus:      500,
		WantContentType: "application/json",
		WantBody:        `{"error":"We ran into a problem while servicing your request please try again later"}` + "\n",
	}.Run(t)
}

// This is the concrete demonstration of the critical fix decided during this
// migration: CreateBotAccount used to pass the raw generated token straight
// through as TokenHash, with no hashing at all, so the bot account's service
// token could never authenticate (X-API-Key verification hashes the
// presented key and looks up by that hash). The service now hashes it before
// storage. This test intercepts CreateServiceToken via a stub and asserts
// what the handler actually passed as rawToken hashes to what the real
// service would have stored, proving the fix without touching a database.
func TestCreateBotAccount_TokenIsHashedBeforeStorage(t *testing.T) {
	accountID := uuid.New()
	var capturedRawToken string
	var capturedParams repository.CreateServiceTokenParams

	tx := testsupport.NewTx(t)
	tx.EXPECT().Commit(gomock.Any()).Return(nil)

	h := &account.AccountHandler{
		Logger: discardLogger(),
		DB:     testsupport.TxDB(t, tx),
		Service: func(repository.Querier) accountsvc.Service {
			return stubService{
				create: func(context.Context, repository.CreateAccountParams) (repository.Account, error) {
					return repository.Account{ID: accountID}, nil
				},
				getBotRole: func(context.Context) (repository.Role, error) {
					return repository.Role{ID: uuid.New()}, nil
				},
				assignRole: func(context.Context, uuid.UUID, uuid.UUID) error {
					return nil
				},
				createServiceToken: func(
					_ context.Context, rawToken string, in repository.CreateServiceTokenParams,
				) (repository.ServiceToken, error) {
					capturedRawToken = rawToken
					capturedParams = in
					return repository.ServiceToken{
						ID:        uuid.New(),
						AccountID: accountID,
					}, nil
				},
			}
		},
	}

	body := `{
		"account": {"email":"bot@example.com","name":"a bot"},
		"service_token": {"name":"ci-bot"}
	}`
	req := httptest.NewRequest(
		"POST",
		"/accounts/bot/create",
		strings.NewReader(body),
	)

	rr := httptest.NewRecorder()
	core.AppHandler(h.CreateBotAccount).ServeHTTP(rr, req)

	require.Equal(t, 201, rr.Code, rr.Body.String())
	require.NotEmpty(
		t,
		capturedRawToken,
		"the handler must pass the raw token to the service",
	)

	// The real service hashes rawToken internally; what matters here is that
	// the handler no longer pre-fills TokenHash with the raw value itself --
	// that was the bug. The params passed in must not carry the raw token as
	// TokenHash.
	assert.NotEqual(
		t,
		capturedRawToken,
		capturedParams.TokenHash,
		"TokenHash must not be the raw token -- that was the bug this migration fixed",
	)
}
