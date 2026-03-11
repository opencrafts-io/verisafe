package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/service"
	mockservice "github.com/opencrafts-io/verisafe/internal/service/mocks"
)

func validInput() service.DeviceRegistrationInput {
	return service.DeviceRegistrationInput{
		UserID:       uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		DeviceName:   "iPhone 15",
		Platform:     "ios",
		PushToken:    "tok_abc123",
		LastActiveAt: "2024-01-15T10:00:00Z",
	}
}

func fakeDeviceRow(
	input service.DeviceRegistrationInput,
) repository.UserDevice {
	name, platform, token := input.DeviceName, input.Platform, input.PushToken
	ts, _ := time.Parse(time.RFC3339, input.LastActiveAt)
	return repository.UserDevice{
		ID:           uuid.MustParse("00000000-0000-0000-0000-000000000099"),
		UserID:       input.UserID,
		DeviceName:   &name,
		Platform:     &platform,
		PushToken:    &token,
		LastActiveAt: pgtype.Timestamp{Time: ts, Valid: true},
		CreatedAt: pgtype.Timestamp{
			Time:  time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			Valid: true,
		},
	}
}

func TestRegisterDevice_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	input := validInput()
	row := fakeDeviceRow(input)

	mockQuerier := mockservice.NewMockQuerier(ctrl)
	mockQuerier.EXPECT().
		RecordUserDevice(gomock.Any(), gomock.Any()).
		Return(row, nil)

	svc := service.NewDeviceService(mockQuerier)
	result, err := svc.RegisterDevice(context.Background(), input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, row.ID, result.ID)
	assert.Equal(t, input.UserID, result.UserID)
	assert.Equal(t, input.DeviceName, result.DeviceName)
	assert.Equal(t, input.Platform, result.Platform)
	assert.Equal(t, input.PushToken, result.PushToken)
	assert.Equal(t, input.LastActiveAt, result.LastActiveAt)
}

func TestRegisterDevice_InvalidTimestamp(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQuerier := mockservice.NewMockQuerier(ctrl)
	mockQuerier.EXPECT().RecordUserDevice(gomock.Any(), gomock.Any()).Times(0)

	input := validInput()
	input.LastActiveAt = "not-a-timestamp"

	svc := service.NewDeviceService(mockQuerier)
	result, err := svc.RegisterDevice(context.Background(), input)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, core.ErrInvalidInput)
}

func TestRegisterDevice_QuerierError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockQuerier := mockservice.NewMockQuerier(ctrl)
	mockQuerier.EXPECT().
		RecordUserDevice(gomock.Any(), gomock.Any()).
		Return(repository.UserDevice{}, errors.New("unique constraint violated"))

	svc := service.NewDeviceService(mockQuerier)
	result, err := svc.RegisterDevice(context.Background(), validInput())

	assert.Nil(t, result)
	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestRegisterDevice_TimestampParsing(t *testing.T) {
	cases := []struct {
		name      string
		timestamp string
		wantErr   bool
	}{
		{"valid UTC", "2024-06-01T12:00:00Z", false},
		{"valid with offset", "2024-06-01T12:00:00+03:00", false},
		{"missing T", "2024-06-01 12:00:00", true},
		{"date only", "2024-06-01", true},
		{"empty string", "", true},
		{"unix epoch string", "1717243200", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockQuerier := mockservice.NewMockQuerier(ctrl)
			if !tc.wantErr {
				mockQuerier.EXPECT().
					RecordUserDevice(gomock.Any(), gomock.Any()).
					Return(fakeDeviceRow(validInput()), nil)
			}

			input := validInput()
			input.LastActiveAt = tc.timestamp

			svc := service.NewDeviceService(mockQuerier)
			_, err := svc.RegisterDevice(context.Background(), input)

			if tc.wantErr {
				assert.ErrorIs(t, err, core.ErrInvalidInput)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRegisterDevice_NilPointersInRow(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	row := repository.UserDevice{
		ID:           uuid.New(),
		UserID:       uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		DeviceName:   nil,
		Platform:     nil,
		PushToken:    nil,
		LastActiveAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		CreatedAt:    pgtype.Timestamp{Time: time.Now(), Valid: true},
	}

	mockQuerier := mockservice.NewMockQuerier(ctrl)
	mockQuerier.EXPECT().
		RecordUserDevice(gomock.Any(), gomock.Any()).
		Return(row, nil)

	svc := service.NewDeviceService(mockQuerier)
	result, err := svc.RegisterDevice(context.Background(), validInput())

	assert.NoError(t, err)
	assert.Equal(t, "", result.DeviceName)
	assert.Equal(t, "", result.Platform)
	assert.Equal(t, "", result.PushToken)
}
