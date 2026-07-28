package eventbus_test

import (
	"context"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/eventbus"
	mockeventbus "github.com/opencrafts-io/verisafe/internal/eventbus/mocks"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// This is the concrete demonstration of what the interfaces in publishers.go
// buy: a handler test can substitute a mock and assert on a published event
// without dialing RabbitMQ, which UserEventBus's constructor otherwise does.
// Before this file, account_handler.go's UserEventBus field held the concrete
// *eventbus.UserEventBus and there was no way to get one in a test at all.

func TestMockUserPublisher_SatisfiesUserPublisher(t *testing.T) {
	ctrl := gomock.NewController(t)
	mock := mockeventbus.NewMockUserPublisher(ctrl)

	var pub eventbus.UserPublisher = mock // compiles only if the mock satisfies it

	mock.EXPECT().
		PublishUserUpdated(gomock.Any(), gomock.Any(), "req-1").
		Return(nil)

	err := pub.PublishUserUpdated(
		context.Background(), repository.Account{}, "req-1",
	)

	assert.NoError(t, err)
}

func TestMockInstitutionPublisher_SatisfiesInstitutionPublisher(t *testing.T) {
	ctrl := gomock.NewController(t)
	mock := mockeventbus.NewMockInstitutionPublisher(ctrl)

	var pub eventbus.InstitutionPublisher = mock

	mock.EXPECT().
		PublishInstitutionCreated(gomock.Any(), gomock.Any(), "req-2").
		Return(nil)

	err := pub.PublishInstitutionCreated(
		context.Background(), repository.Institution{}, "req-2",
	)

	assert.NoError(t, err)
}

func TestMockNotificationPublisher_SatisfiesNotificationPublisher(t *testing.T) {
	ctrl := gomock.NewController(t)
	mock := mockeventbus.NewMockNotificationPublisher(ctrl)

	var pub eventbus.NotificationPublisher = mock

	mock.EXPECT().
		PublishPushNotificationRequested(gomock.Any(), gomock.Any(), "req-3").
		Return(nil)

	err := pub.PublishPushNotificationRequested(
		context.Background(), eventbus.NotificationPayload{}, "req-3",
	)

	assert.NoError(t, err)
}
