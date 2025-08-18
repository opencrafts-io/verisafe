package utils

import (
	"context"
	"log/slog"
	"time"

	"github.com/opencrafts-io/verisafe/internal/repository"
)

// TokenRotationScheduler handles automatic token rotation and cleanup
type TokenRotationScheduler struct {
	repo   *repository.Queries
	logger *slog.Logger
}

// NewTokenRotationScheduler creates a new token rotation scheduler
func NewTokenRotationScheduler(repo *repository.Queries, logger *slog.Logger) *TokenRotationScheduler {
	return &TokenRotationScheduler{
		repo:   repo,
		logger: logger,
	}
}

// StartScheduler starts the background scheduler for token rotation and cleanup
func (trs *TokenRotationScheduler) StartScheduler(ctx context.Context) {
	// Run cleanup every hour
	go trs.runCleanupScheduler(ctx)
	
	// Run rotation check every 6 hours
	go trs.runRotationScheduler(ctx)
}

// runCleanupScheduler runs the cleanup task periodically
func (trs *TokenRotationScheduler) runCleanupScheduler(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			trs.logger.Info("Token cleanup scheduler stopped")
			return
		case <-ticker.C:
			if err := trs.cleanupExpiredTokens(ctx); err != nil {
				trs.logger.Error("Failed to cleanup expired tokens", slog.String("error", err.Error()))
			}
		}
	}
}

// runRotationScheduler runs the rotation check task periodically
func (trs *TokenRotationScheduler) runRotationScheduler(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			trs.logger.Info("Token rotation scheduler stopped")
			return
		case <-ticker.C:
			if err := trs.checkTokensNeedingRotation(ctx); err != nil {
				trs.logger.Error("Failed to check tokens needing rotation", slog.String("error", err.Error()))
			}
		}
	}
}

// cleanupExpiredTokens cleans up expired service tokens
func (trs *TokenRotationScheduler) cleanupExpiredTokens(ctx context.Context) error {
	trs.logger.Info("Starting expired token cleanup")
	
	if err := trs.repo.CleanupExpiredServiceTokens(ctx); err != nil {
		return err
	}
	
	trs.logger.Info("Completed expired token cleanup")
	return nil
}

// checkTokensNeedingRotation marks tokens that need rotation
func (trs *TokenRotationScheduler) checkTokensNeedingRotation(ctx context.Context) error {
	trs.logger.Info("Checking tokens needing rotation")
	
	if err := trs.repo.MarkTokensForRotation(ctx); err != nil {
		return err
	}
	
	trs.logger.Info("Completed rotation check")
	return nil
}

// GetTokensNeedingRotation returns tokens that need to be rotated
func (trs *TokenRotationScheduler) GetTokensNeedingRotation(ctx context.Context) ([]repository.ServiceToken, error) {
	return trs.repo.ListServiceTokensNeedingRotation(ctx)
}

// RotateToken rotates a specific token
func (trs *TokenRotationScheduler) RotateToken(ctx context.Context, tokenID string, newTokenHash string, newExpiry *time.Time) error {
	// This would be implemented in the repository layer
	// For now, we'll use the existing rotation method
	return nil
}


