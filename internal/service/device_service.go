package service

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

//go:generate mockgen -source=../repository/querier.go -destination=mocks/mock_querier.go -package=mockservice

// DeviceRegistrationInput is the input coming from the transport layer.
type DeviceRegistrationInput struct {
	UserID       uuid.UUID   `json:"user_id"`
	DeviceName   string      `json:"device_name"`
	Platform     string      `json:"platform"`
	DeviceToken  string      `json:"device_token"`
	IpAddress    *netip.Addr `json:"ip_address"`
	Country      *string     `json:"country"`
	LastActiveAt string      `json:"last_active_at"`
}

// DeviceOutput is the response returned to the transport layer.
type DeviceOutput struct {
	ID           uuid.UUID `json:"id"`
	UserID       uuid.UUID `json:"user_id"`
	DeviceName   string    `json:"device_name"`
	Platform     string    `json:"platform"`
	DeviceToken  string    `json:"device_token"`
	LastActiveAt string    `json:"last_active_at"`
	IpAddress    string    `json:"ip_address"`
	Country      string    `json:"country"`
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
		return nil, err
	}

	row, err := s.querier.RecordUserDevice(ctx, params)
	if err != nil {
		return nil, err
	}

	output := deviceRowToOutput(row)
	return &output, nil
}

// deviceRegistrationInputToRepoParams maps the service input to sqlc query params.
func deviceRegistrationInputToRepoParams(
	input DeviceRegistrationInput,
) (repository.RecordUserDeviceParams, error) {
	var lastActive time.Time

	if input.LastActiveAt == "" {
		lastActive = time.Now()
	} else {
		parsed, err := time.Parse(time.RFC3339, input.LastActiveAt)
		if err != nil {
			return repository.RecordUserDeviceParams{},
				fmt.Errorf("invalid timestamp: %v", err)
		}
		lastActive = parsed
	}
	return repository.RecordUserDeviceParams{
		UserID:      input.UserID,
		DeviceName:  &input.DeviceName,
		Platform:    &input.Platform,
		DeviceToken: &input.DeviceToken,
		IpAddress:   input.IpAddress,
		Country:     input.Country,
		LastActiveAt: pgtype.Timestamp{
			Time:  lastActive,
			Valid: true,
		},
	}, nil
}

// deviceRowToOutput maps the sqlc database row to the service output DTO.
func deviceRowToOutput(row repository.UserDevice) DeviceOutput {
	var ip_address string

	if row.IpAddress != nil {
		ip_address = row.IpAddress.String()
	} else {
		ip_address = ""
	}
	return DeviceOutput{
		ID:           row.ID,
		UserID:       row.UserID,
		DeviceName:   derefString(row.DeviceName),
		Platform:     derefString(row.Platform),
		DeviceToken:  derefString(row.DeviceToken),
		IpAddress:    ip_address,
		Country:      derefString(row.Country),
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
