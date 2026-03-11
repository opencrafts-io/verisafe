package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

//go:generate mockgen -source=../repository/querier.go -destination=mocks/mock_querier.go -package=mockservice

// DeviceRegistrationInput is the input coming from the transport layer.
type DeviceRegistrationInput struct {
	UserID       uuid.UUID `json:"user_id"`
	DeviceName   string    `json:"device_name"`
	Platform     string    `json:"platform"`
	PushToken    string    `json:"push_token"`
	LastActiveAt string    `json:"last_active_at"`
}

// DeviceOutput is the response returned to the transport layer.
type DeviceOutput struct {
	ID           uuid.UUID `json:"id"`
	UserID       uuid.UUID `json:"user_id"`
	DeviceName   string    `json:"device_name"`
	Platform     string    `json:"platform"`
	PushToken    string    `json:"push_token"`
	LastActiveAt string    `json:"last_active_at"`
	CreatedAt    string    `json:"created_at"`
}

type DeviceService interface {
	RegisterDevice(
		ctx context.Context,
		input DeviceRegistrationInput,
	) (*DeviceOutput, error)
}

type deviceService struct {
	querier repository.Querier
}

func NewDeviceService(q repository.Querier) DeviceService {
	return &deviceService{querier: q}
}

func (s *deviceService) RegisterDevice(
	ctx context.Context,
	input DeviceRegistrationInput,
) (*DeviceOutput, error) {
	params, err := deviceRegistrationInputToRepoParams(input)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", core.ErrInvalidInput, err)
	}

	row, err := s.querier.RecordUserDevice(ctx, params)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: failed to persist device: %v",
			core.ErrInternal,
			err,
		)
	}

	output := deviceRowToOutput(row)
	return &output, nil
}

// deviceRegistrationInputToRepoParams maps the service input to sqlc query params.
func deviceRegistrationInputToRepoParams(
	input DeviceRegistrationInput,
) (repository.RecordUserDeviceParams, error) {
	lastActive, err := time.Parse(time.RFC3339, input.LastActiveAt)
	if err != nil {
		return repository.RecordUserDeviceParams{}, fmt.Errorf(
			"invalid timestamp format: %v",
			err,
		)
	}

	return repository.RecordUserDeviceParams{
		UserID:     input.UserID,
		DeviceName: &input.DeviceName,
		Platform:   &input.Platform,
		PushToken:  &input.PushToken,
		LastActiveAt: pgtype.Timestamp{
			Time:  lastActive,
			Valid: true,
		},
	}, nil
}

// deviceRowToOutput maps the sqlc database row to the service output DTO.
func deviceRowToOutput(row repository.UserDevice) DeviceOutput {
	return DeviceOutput{
		ID:           row.ID,
		UserID:       row.UserID,
		DeviceName:   derefString(row.DeviceName),
		Platform:     derefString(row.Platform),
		PushToken:    derefString(row.PushToken),
		LastActiveAt: row.LastActiveAt.Time.Format(time.RFC3339),
		CreatedAt:    row.CreatedAt.Time.Format(time.RFC3339),
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
