package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/providers"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/secrets"
)

// reconcilerLeaderKey elects one replica to run the worker. Every instance
// tries to take it each tick and only the holder does work, so running several
// replicas does not multiply provider traffic.
const reconcilerLeaderKey = "oauth:reconciler:leader"

// OAuthReconciler converts presumed scope grants into provider-verified ones.
//
// It exists because of a hard limit: providers offer no "what did user X
// grant" API. Scope is reported only in a token response, so the only way to
// learn the truth about a grant migrated from the old socials table is to
// refresh it. Grants belonging to active users verify themselves on the first
// broker call; this worker exists solely to drain the long tail of accounts
// nobody brokers a token for, so that the transitional plaintext columns can
// eventually be dropped.
//
// It is explicitly NOT a token-warming job. Refreshing every user's token on a
// schedule would generate tens of thousands of pointless provider calls a day
// and buy nothing — an access token is only needed at the moment a service
// asks for one, and refreshing lazily costs that one request a few hundred
// milliseconds. Once the presumed population reaches zero this worker can be
// switched off permanently.
type OAuthReconciler struct {
	db        core.IDBProvider
	cacher    core.Cacher
	registry  *providers.Registry
	sealer    *secrets.Sealer
	exchanger providers.TokenExchanger
	cfg       *config.Config
	logger    *slog.Logger

	// instanceID identifies this replica in the leader lock.
	instanceID string
	// newService is swappable in tests.
	newService func(repository.Querier) GrantService
}

// NewOAuthReconciler builds the worker.
func NewOAuthReconciler(
	db core.IDBProvider,
	cacher core.Cacher,
	registry *providers.Registry,
	sealer *secrets.Sealer,
	exchanger providers.TokenExchanger,
	cfg *config.Config,
	logger *slog.Logger,
) *OAuthReconciler {
	r := &OAuthReconciler{
		db:         db,
		cacher:     cacher,
		registry:   registry,
		sealer:     sealer,
		exchanger:  exchanger,
		cfg:        cfg,
		logger:     logger,
		instanceID: uuid.NewString(),
	}
	r.newService = func(q repository.Querier) GrantService {
		return NewGrantService(q, cacher, registry, sealer, exchanger, cfg, logger)
	}
	return r
}

// tickInterval is how often the worker wakes. One minute pairs with the
// per-minute rate limit so each tick processes at most one batch.
const tickInterval = time.Minute

// Run drives the worker until ctx is cancelled.
func (r *OAuthReconciler) Run(ctx context.Context) {
	r.logger.Info(
		"oauth reconciler started",
		slog.String("instance_id", r.instanceID),
		slog.Int("rate_per_minute", r.cfg.ProviderTokensConfig.ReconcileRatePerMin),
	)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("oauth reconciler stopped")
			return
		case <-ticker.C:
			if !r.acquireLeadership(ctx) {
				continue
			}
			if err := r.runBatch(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.logger.Error("oauth reconciler batch failed", slog.Any("error", err))
			}
		}
	}
}

// acquireLeadership takes or renews the leader lock. The lease outlives the
// tick so a slow batch does not hand leadership to another replica mid-run.
func (r *OAuthReconciler) acquireLeadership(ctx context.Context) bool {
	won, err := r.cacher.SetNX(ctx, reconcilerLeaderKey, r.instanceID, 90*time.Second)
	if err != nil {
		r.logger.Warn("reconciler leader election failed", slog.Any("error", err))
		return false
	}
	if won {
		return true
	}

	// Already the leader? Renew rather than skip.
	var holder string
	if err := r.cacher.Get(ctx, reconcilerLeaderKey, &holder); err != nil {
		return false
	}
	if holder != r.instanceID {
		return false
	}
	if err := r.cacher.Set(ctx, reconcilerLeaderKey, r.instanceID, 90*time.Second); err != nil {
		r.logger.Warn("reconciler leader renewal failed", slog.Any("error", err))
	}
	return true
}

// runBatch reconciles up to the configured per-minute budget.
func (r *OAuthReconciler) runBatch(ctx context.Context) error {
	limit := int32(r.cfg.ProviderTokensConfig.ReconcileRatePerMin)
	if limit <= 0 {
		return nil
	}

	conn, err := r.db.Acquire(ctx)
	if err != nil {
		return err
	}

	var pending []repository.OauthGrant
	err = core.WithTransaction(ctx, conn, func(tx pgx.Tx) error {
		var innerErr error
		pending, innerErr = repository.New(tx).ListUnverifiedOAuthGrants(ctx, limit)
		return innerErr
	})
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	r.logger.Info("reconciling unverified oauth grants", slog.Int("count", len(pending)))

	var verified, failed int
	for _, grant := range pending {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := r.reconcileOne(ctx, grant.ID); err != nil {
			failed++
			// Expected outcomes, not incidents: a provider that cannot report
			// scopes will never verify, and a dead grant is already recorded.
			if errors.Is(err, ErrRefreshUnsupported) || errors.Is(err, ErrGrantRevoked) {
				continue
			}
			r.logger.Warn(
				"failed to reconcile oauth grant",
				slog.String("grant_id", grant.ID.String()),
				slog.String("provider", grant.Provider),
				slog.Any("error", err),
			)
			// Back off entirely on a provider outage rather than burning the
			// whole batch against an endpoint that is already failing.
			if errors.Is(err, ErrProviderUnavailable) {
				return nil
			}
			continue
		}
		verified++
	}

	r.logger.Info(
		"oauth grant reconciliation batch complete",
		slog.Int("verified", verified),
		slog.Int("failed", failed),
	)
	return nil
}

func (r *OAuthReconciler) reconcileOne(ctx context.Context, grantID uuid.UUID) error {
	conn, err := r.db.Acquire(ctx)
	if err != nil {
		return err
	}

	return core.WithTransaction(ctx, conn, func(tx pgx.Tx) error {
		return r.newService(repository.New(tx)).Reconcile(ctx, grantID)
	})
}
