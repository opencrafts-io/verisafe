package billing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	billingSvc "github.com/opencrafts-io/verisafe/internal/service/billing"
	"github.com/opencrafts-io/verisafe/internal/tokens"
)

const (
	checkoutHandoffPrefix = "checkout_handoff:"
	checkoutHandoffTTL    = 5 * time.Minute
	checkoutTokenTTL      = 15 * time.Minute
)

type CheckoutHandler struct {
	DB            core.IDBProvider
	Logger        *slog.Logger
	Cfg           *config.Config
	Cacher        core.Cacher
	ChargeService billingSvc.ChargeService
}

type createCheckoutSessionRequest struct {
	OrderID string `json:"order_id"`
}

type checkoutSessionResponse struct {
	CheckoutURL string    `json:"checkout_url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type exchangeCheckoutSessionRequest struct {
	Code string `json:"code"`
}

type checkoutTokenResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
	OrderID     string    `json:"order_id"`
}

type checkoutHandoff struct {
	UserID    uuid.UUID `json:"user_id"`
	OrderID   string    `json:"order_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (h *CheckoutHandler) RegisterHandlers(router core.Router) {
	router.Handle(
		"POST /checkout-sessions",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.Cfg, h.DB, h.Cacher, h.Logger),
			middleware.HasAnyPermission([]string{
				"create:checkout-session:own",
				"create:checkout-session:any",
			}),
		)(core.AppHandler(h.CreateCheckoutSession)),
	)

	router.Handle(
		"POST /checkout-sessions/exchange",
		core.AppHandler(h.ExchangeCheckoutSession),
	)

	router.Handle(
		"GET /checkout/orders/{order_id}",
		core.AppHandler(h.GetCheckoutOrder),
	)

	router.Handle(
		"GET /checkout/orders/{order_id}/items",
		core.AppHandler(h.ListCheckoutOrderItems),
	)

	router.Handle(
		"POST /checkout/orders/{order_id}/charge",
		core.AppHandler(h.ChargeCheckoutOrder),
	)
}

func (h *CheckoutHandler) CreateCheckoutSession(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req createCheckoutSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		strings.TrimSpace(req.OrderID) == "" {
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}

	userID, err := orderCallerID(r)
	if err != nil {
		return err
	}

	if err := h.authorizeCheckoutOrder(r, req.OrderID, userID); err != nil {
		if errors.Is(err, billingSvc.ErrOrderNotFound) {
			return core.Public(core.ErrNotFound, msgOrderNotFound)
		}
		return err
	}

	code, err := generateCheckoutToken()
	if err != nil {
		return core.Public(core.ErrInternal, "Failed to create checkout session.")
	}

	expiresAt := time.Now().Add(checkoutHandoffTTL)
	err = h.Cacher.Set(
		r.Context(),
		checkoutCacheKey(checkoutHandoffPrefix, code),
		checkoutHandoff{
			UserID:    userID,
			OrderID:   req.OrderID,
			ExpiresAt: expiresAt,
		},
		checkoutHandoffTTL,
	)
	if err != nil {
		h.Logger.ErrorContext(
			r.Context(),
			"failed to store checkout handoff",
			slog.Any("error", err),
			slog.String("order_id", req.OrderID),
		)
		return core.Public(core.ErrInternal, "Failed to create checkout session.")
	}

	core.WriteJSON(w, http.StatusCreated, checkoutSessionResponse{
		CheckoutURL: checkoutStartURL(r, code),
		ExpiresAt:   expiresAt,
	})
	return nil
}

func (h *CheckoutHandler) ExchangeCheckoutSession(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req exchangeCheckoutSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		return core.Public(core.ErrInvalidInput, "Missing checkout code.")
	}

	key := checkoutCacheKey(checkoutHandoffPrefix, req.Code)
	var handoff checkoutHandoff
	if err := h.Cacher.Get(r.Context(), key, &handoff); err != nil {
		if errors.Is(err, core.ErrCacheMiss) {
			return core.Public(core.ErrUnauthorized, "Invalid or expired checkout code.")
		}
		return core.Public(core.ErrInternal, "Failed to start checkout session.")
	}

	if err := h.Cacher.Delete(r.Context(), key); err != nil {
		h.Logger.WarnContext(
			r.Context(),
			"failed to delete checkout handoff",
			slog.Any("error", err),
		)
	}

	token, err := tokens.NewTokenService(
		nil,
		h.Cacher,
		h.Cfg,
	).IssueCheckoutToken(
		r.Context(),
		handoff.UserID,
		handoff.OrderID,
		checkoutTokenTTL,
	)
	if err != nil {
		return core.Public(core.ErrInternal, "Failed to start checkout session.")
	}

	core.WriteJSON(w, http.StatusOK, checkoutTokenResponse{
		AccessToken: token.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   token.ExpiresAt,
		OrderID:     token.OrderID,
	})
	return nil
}

func (h *CheckoutHandler) GetCheckoutOrder(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, err := h.authorizeCheckoutRequest(
		r,
		"checkout:order:read",
		"read:order:own",
		"read:order:any",
	)
	if err != nil {
		return err
	}

	order, err := core.InTx(
		r.Context(),
		h.DB,
		func(tx pgx.Tx) (*billingSvc.Order, error) {
			return billingSvc.NewOrderService(
				repository.New(tx),
				h.Logger,
			).GetUserOrder(
				r.Context(),
				billingSvc.GetUserOrder{
					ID:     claims.OrderID,
					UserID: claims.UserID,
				},
			)
		},
	)
	if err != nil {
		if errors.Is(err, billingSvc.ErrOrderNotFound) {
			return core.Public(core.ErrNotFound, msgOrderNotFound)
		}
		return core.Public(core.ErrInternal, msgFetchOrderFailed)
	}

	core.WriteJSON(w, http.StatusOK, order)
	return nil
}

func (h *CheckoutHandler) ListCheckoutOrderItems(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, err := h.authorizeCheckoutRequest(
		r,
		"checkout:order-item:read",
		"read:order-item:own",
		"read:order-item:any",
	)
	if err != nil {
		return err
	}

	items, err := core.InTx(
		r.Context(),
		h.DB,
		func(tx pgx.Tx) ([]billingSvc.OrderItem, error) {
			if _, err := billingSvc.NewOrderService(
				repository.New(tx),
				h.Logger,
			).GetUserOrder(
				r.Context(),
				billingSvc.GetUserOrder{
					ID:     claims.OrderID,
					UserID: claims.UserID,
				},
			); err != nil {
				return nil, err
			}

			return billingSvc.NewOrderItemService(
				repository.New(tx),
				h.Logger,
			).ListOrderItemsByOrder(
				r.Context(),
				billingSvc.ListOrderItemsByOrder{OrderID: claims.OrderID},
			)
		},
	)
	if err != nil {
		if errors.Is(err, billingSvc.ErrOrderNotFound) {
			return core.Public(core.ErrNotFound, msgOrderNotFound)
		}
		return core.Public(core.ErrInternal, msgFetchOrderItemsFailed)
	}

	core.WriteJSON(w, http.StatusOK, items)
	return nil
}

func (h *CheckoutHandler) ChargeCheckoutOrder(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, err := h.authorizeCheckoutRequest(
		r,
		"checkout:order:charge",
		"update:order:own",
		"update:order:any",
	)
	if err != nil {
		return err
	}

	var req billingSvc.ChargeOrder
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return core.Public(core.ErrInvalidInput, msgInvalidBody)
	}
	req.OrderID = claims.OrderID

	attempt, err := h.ChargeService.ChargeOrder(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, billingSvc.ErrOrderNotFound):
			return core.Public(core.ErrNotFound, msgOrderNotFound)
		case errors.Is(err, billingSvc.ErrOrderAlreadyPaid):
			return core.Public(core.ErrConflict, msgOrderAlreadyPaid)
		case errors.Is(err, billingSvc.ErrOrderCancelled):
			return core.Public(core.ErrConflict, msgOrderCancelled)
		case errors.Is(err, billingSvc.ErrPendingChargeAttempt):
			return core.Public(core.ErrConflict, msgPendingChargeAttempt)
		case errors.Is(err, billingSvc.ErrInvalidChargeAmount),
			errors.Is(err, billingSvc.ErrInvalidPayerPhone),
			errors.Is(err, billingSvc.ErrOrderNotChargeable):
			return core.Public(core.ErrInvalidInput, msgOrderNotChargeable)
		case errors.Is(err, billingSvc.ErrChargePublishFailed):
			return core.Public(core.ErrUnavailable, msgChargeUnavailable)
		default:
			return core.Public(core.ErrInternal, msgChargeOrderFailed)
		}
	}

	core.WriteJSON(w, http.StatusAccepted, attempt)
	return nil
}

func (h *CheckoutHandler) authorizeCheckoutOrder(
	r *http.Request,
	orderID string,
	userID uuid.UUID,
) error {
	return core.InTxDo(
		r.Context(),
		h.DB,
		func(tx pgx.Tx) error {
			svc := billingSvc.NewOrderService(repository.New(tx), h.Logger)
			if middleware.HasContextPermission(r.Context(), "create:checkout-session:any") {
				_, err := svc.GetOrder(
					r.Context(),
					billingSvc.GetOrder{ID: orderID},
				)
				return err
			}

			_, err := svc.GetUserOrder(
				r.Context(),
				billingSvc.GetUserOrder{
					ID:     orderID,
					UserID: userID,
				},
			)
			return err
		},
	)
}

type checkoutRequestClaims struct {
	UserID  uuid.UUID
	OrderID string
}

func (h *CheckoutHandler) authorizeCheckoutRequest(
	r *http.Request,
	checkoutScope string,
	ownPermission string,
	anyPermission string,
) (checkoutRequestClaims, error) {
	rawToken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if rawToken == "" || rawToken == r.Header.Get("Authorization") {
		return checkoutRequestClaims{}, core.Public(
			core.ErrUnauthorized,
			"Missing checkout token.",
		)
	}

	tokenSvc := tokens.NewTokenService(
		nil,
		h.Cacher,
		h.Cfg,
	)
	claims, err := tokenSvc.ValidateCheckoutToken(
		r.Context(),
		rawToken,
	)
	if err == nil {
		return checkoutClaimsFromScopedToken(r, claims, checkoutScope)
	}
	if checkoutTokenLikely(rawToken) {
		return checkoutRequestClaims{}, core.Public(
			core.ErrUnauthorized,
			"Invalid or expired checkout token.",
		)
	}

	accessClaims, err := tokenSvc.ValidateAccessToken(r.Context(), rawToken)
	if err != nil {
		return checkoutRequestClaims{}, core.Public(
			core.ErrUnauthorized,
			"Invalid or expired checkout token.",
		)
	}
	userID, err := uuid.Parse(accessClaims.Subject)
	if err != nil {
		return checkoutRequestClaims{}, core.Public(
			core.ErrUnauthorized,
			"Invalid checkout token subject.",
		)
	}

	return h.checkoutClaimsFromAccessToken(
		r,
		userID,
		ownPermission,
		anyPermission,
	)
}

func checkoutClaimsFromScopedToken(
	r *http.Request,
	claims *tokens.CheckoutClaims,
	requiredScope string,
) (checkoutRequestClaims, error) {
	if claims.OrderID != r.PathValue("order_id") {
		return checkoutRequestClaims{}, core.Public(
			core.ErrUnauthorized,
			"Checkout token does not match this order.",
		)
	}
	if requiredScope != "" && !slices.Contains(claims.Scopes, requiredScope) {
		return checkoutRequestClaims{}, core.Public(
			core.ErrForbidden,
			"Checkout token cannot perform this action.",
		)
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return checkoutRequestClaims{}, core.Public(
			core.ErrUnauthorized,
			"Invalid checkout token subject.",
		)
	}
	return checkoutRequestClaims{UserID: userID, OrderID: claims.OrderID}, nil
}

func (h *CheckoutHandler) checkoutClaimsFromAccessToken(
	r *http.Request,
	userID uuid.UUID,
	ownPermission string,
	anyPermission string,
) (checkoutRequestClaims, error) {
	orderID := r.PathValue("order_id")
	err := core.InTxDo(
		r.Context(),
		h.DB,
		func(tx pgx.Tx) error {
			repo := repository.New(tx)
			perms, err := repo.GetUserPermissionNames(r.Context(), userID)
			if err != nil {
				return err
			}

			orderSvc := billingSvc.NewOrderService(repo, h.Logger)
			if slices.Contains(perms, anyPermission) {
				_, err := orderSvc.GetOrder(r.Context(), billingSvc.GetOrder{
					ID: orderID,
				})
				return err
			}
			if !slices.Contains(perms, ownPermission) {
				return core.ErrForbidden
			}

			_, err = orderSvc.GetUserOrder(
				r.Context(),
				billingSvc.GetUserOrder{
					ID:     orderID,
					UserID: userID,
				},
			)
			return err
		},
	)
	if err != nil {
		if errors.Is(err, billingSvc.ErrOrderNotFound) {
			return checkoutRequestClaims{}, core.Public(
				core.ErrNotFound,
				msgOrderNotFound,
			)
		}
		if errors.Is(err, core.ErrForbidden) {
			return checkoutRequestClaims{}, core.Public(
				core.ErrForbidden,
				"Checkout token cannot perform this action.",
			)
		}
		return checkoutRequestClaims{}, core.Public(
			core.ErrInternal,
			"Failed to validate checkout token.",
		)
	}

	return checkoutRequestClaims{UserID: userID, OrderID: orderID}, nil
}

func checkoutTokenLikely(rawToken string) bool {
	token, _, err := new(jwt.Parser).ParseUnverified(
		rawToken,
		&tokens.CheckoutClaims{},
	)
	if err != nil {
		return false
	}

	checkoutClaims, ok := token.Claims.(*tokens.CheckoutClaims)
	return ok && checkoutClaims.TokenType == tokens.CheckoutTokenType
}

func checkoutStartURL(r *http.Request, code string) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "https"
		if r.TLS == nil && strings.HasPrefix(r.Host, "localhost") {
			scheme = "http"
		}
	}

	return fmt.Sprintf("%s://%s/checkout/start?code=%s", scheme, r.Host, code)
}

func checkoutCacheKey(prefix string, token string) string {
	sum := sha256.Sum256([]byte(token))
	return prefix + base64.RawURLEncoding.EncodeToString(sum[:])
}

func generateCheckoutToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
