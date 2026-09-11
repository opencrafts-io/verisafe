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

type CreateCheckoutSessionRequest struct {
	OrderID string `json:"order_id"`
}

type CheckoutSessionResponse struct {
	CheckoutURL string    `json:"checkout_url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type ExchangeCheckoutSessionRequest struct {
	Code string `json:"code"`
}

type CheckoutTokenResponse struct {
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

// CreateCheckoutSession godoc
//
// @Summary      Create a checkout session
// @Description  Creates a short-lived browser checkout handoff for an unpaid order.
// @Tags         checkout
// @Accept       json
// @Produce      json
// @Param        request  body      CreateCheckoutSessionRequest  true  "Order to check out"
// @Success      201      {object}  CheckoutSessionResponse
// @Failure      400      {object}  core.APIError  "Invalid request body"
// @Failure      401      {object}  core.APIError  "Missing or invalid claims"
// @Failure      404      {object}  core.APIError  "Order not found"
// @Failure      500      {object}  core.APIError  "Failed to create checkout session"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /checkout-sessions [post]
func (h *CheckoutHandler) CreateCheckoutSession(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req CreateCheckoutSessionRequest
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

	core.WriteJSON(w, http.StatusCreated, CheckoutSessionResponse{
		CheckoutURL: checkoutStartURL(r, code),
		ExpiresAt:   expiresAt,
	})
	return nil
}

// ExchangeCheckoutSession godoc
//
// @Summary      Exchange a checkout session code
// @Description  Exchanges a one-time checkout handoff code for a short-lived checkout token.
// @Tags         checkout
// @Accept       json
// @Produce      json
// @Param        request  body      ExchangeCheckoutSessionRequest  true  "Checkout handoff code"
// @Success      200      {object}  CheckoutTokenResponse
// @Failure      400      {object}  core.APIError  "Missing checkout code"
// @Failure      401      {object}  core.APIError  "Invalid or expired checkout code"
// @Failure      500      {object}  core.APIError  "Failed to start checkout session"
// @Router       /checkout-sessions/exchange [post]
func (h *CheckoutHandler) ExchangeCheckoutSession(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req ExchangeCheckoutSessionRequest
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

	core.WriteJSON(w, http.StatusOK, CheckoutTokenResponse{
		AccessToken: token.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   token.ExpiresAt,
		OrderID:     token.OrderID,
	})
	return nil
}

// GetCheckoutOrder godoc
//
// @Summary      Get a checkout order
// @Description  Retrieves the order bound to the checkout token or authorized access token.
// @Tags         checkout
// @Produce      json
// @Param        order_id  path      string  true  "Order ID"
// @Success      200       {object}  billingSvc.Order
// @Failure      401       {object}  core.APIError  "Missing or invalid checkout token"
// @Failure      403       {object}  core.APIError  "Insufficient checkout scope or permission"
// @Failure      404       {object}  core.APIError  "Order not found"
// @Failure      500       {object}  core.APIError  "Failed to fetch order"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /checkout/orders/{order_id} [get]
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

// ListCheckoutOrderItems godoc
//
// @Summary      List checkout order items
// @Description  Lists items for the order bound to the checkout token or authorized access token.
// @Tags         checkout
// @Produce      json
// @Param        order_id  path      string  true  "Order ID"
// @Success      200       {array}   billingSvc.OrderItem
// @Failure      401       {object}  core.APIError  "Missing or invalid checkout token"
// @Failure      403       {object}  core.APIError  "Insufficient checkout scope or permission"
// @Failure      404       {object}  core.APIError  "Order not found"
// @Failure      500       {object}  core.APIError  "Failed to fetch order items"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /checkout/orders/{order_id}/items [get]
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

// ChargeCheckoutOrder godoc
//
// @Summary      Charge a checkout order
// @Description  Requests an M-Pesa STK charge for the order bound to the checkout token or authorized access token.
// @Tags         checkout
// @Accept       json
// @Produce      json
// @Param        order_id  path      string                  true  "Order ID"
// @Param        request   body      billingSvc.ChargeOrder  true  "Payer phone number"
// @Success      202       {object}  billingSvc.ChargeAttempt
// @Failure      400       {object}  core.APIError  "Invalid request body or order"
// @Failure      401       {object}  core.APIError  "Missing or invalid checkout token"
// @Failure      403       {object}  core.APIError  "Insufficient checkout scope or permission"
// @Failure      404       {object}  core.APIError  "Order not found"
// @Failure      409       {object}  core.APIError  "Order cannot be charged"
// @Failure      503       {object}  core.APIError  "RabbitMQ unavailable"
// @Failure      500       {object}  core.APIError  "Failed to charge order"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /checkout/orders/{order_id}/charge [post]
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
