package social_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/opencrafts-io/verisafe/internal/service/social"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newService(t *testing.T) (social.Service, *mockQuerier.MockQuerier) {
	t.Helper()
	q := mockQuerier.NewMockQuerier(gomock.NewController(t))
	return social.NewService(q), q
}

// sqlc emits items := []Social{} for an empty result (emit_empty_slices), so
// the endpoint serialises []. Rebuilding the slice would turn that into null.
func TestListForAccount_EmptyResultStaysAnEmptySliceNotNil(t *testing.T) {
	svc, q := newService(t)
	accountID := uuid.New()
	q.EXPECT().GetAllAccountSocials(gomock.Any(), accountID).
		Return([]repository.Social{}, nil)

	got, err := svc.ListForAccount(context.Background(), accountID)

	require.NoError(t, err)
	assert.NotNil(t, got, "an empty result must marshal to [] and not null")
	assert.Empty(t, got)
}

func TestListForAccount_ReturnsRowsUnchanged(t *testing.T) {
	svc, q := newService(t)
	accountID := uuid.New()
	want := []repository.Social{{AccountID: accountID, Provider: "google"}}
	q.EXPECT().GetAllAccountSocials(gomock.Any(), accountID).Return(want, nil)

	got, err := svc.ListForAccount(context.Background(), accountID)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestListForAccount_DriverErrorIsInternal(t *testing.T) {
	svc, q := newService(t)
	accountID := uuid.New()
	q.EXPECT().GetAllAccountSocials(gomock.Any(), accountID).
		Return(nil, errors.New("connection reset"))

	_, err := svc.ListForAccount(context.Background(), accountID)

	assert.ErrorIs(t, err, core.ErrInternal)
}
