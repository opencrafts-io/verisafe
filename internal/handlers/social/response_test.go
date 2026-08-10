package social

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr(s string) *string { return &s }

func fullSocial() repository.Social {
	return repository.Social{
		UserID:            "109348572093485720394",
		IDToken:           ptr("eyJhbGciOi.id.token"),
		AccountID:         uuid.MustParse("9f1c8b2e-0000-4000-8000-000000000001"),
		Provider:          "google",
		Email:             ptr("user@example.com"),
		Name:              ptr("Ada Lovelace"),
		FirstName:         ptr("Ada"),
		LastName:          ptr("Lovelace"),
		NickName:          ptr("ada"),
		Description:       ptr("a description"),
		AvatarUrl:         ptr("https://example.com/a.png"),
		Location:          ptr("Nairobi"),
		AccessToken:       ptr("ya29.a0AfB_REAL_ACCESS_TOKEN"),
		AccessTokenSecret: ptr("REAL_TOKEN_SECRET"),
		RefreshToken:      ptr("1//0gK9_REAL_REFRESH_TOKEN"),
	}
}

// The bug this guards: both /socials endpoints used to serialize
// repository.Social directly, so a user's Google refresh token was returned to
// any caller holding read:account:own — i.e. every user — and every user's to
// any holder of read:account:any.
func TestSanitizeSocials_OmitsCredentials(t *testing.T) {
	out := sanitizeSocials([]repository.Social{fullSocial()})
	require.Len(t, out, 1)

	assert.Nil(t, out[0].AccessToken)
	assert.Nil(t, out[0].AccessTokenSecret)
	assert.Nil(t, out[0].RefreshToken)

	raw, err := json.Marshal(out[0])
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, field := range []string{"access_token", "access_token_secret", "refresh_token"} {
		value, present := decoded[field]
		assert.True(t, present, "%s must remain present so existing decoders keep working", field)
		assert.Nil(t, value, "%s must serialize as null", field)
	}

	// Nothing that looks like a credential should survive anywhere in the body.
	assert.NotContains(t, string(raw), "REAL_ACCESS_TOKEN")
	assert.NotContains(t, string(raw), "REAL_TOKEN_SECRET")
	assert.NotContains(t, string(raw), "REAL_REFRESH_TOKEN")
}

func TestSanitizeSocials_PreservesEverythingElse(t *testing.T) {
	in := fullSocial()
	got := sanitizeSocial(in)

	assert.Equal(t, in.UserID, got.UserID)
	assert.Equal(t, in.IDToken, got.IDToken)
	assert.Equal(t, in.AccountID, got.AccountID)
	assert.Equal(t, in.Provider, got.Provider)
	assert.Equal(t, in.Email, got.Email)
	assert.Equal(t, in.Name, got.Name)
	assert.Equal(t, in.FirstName, got.FirstName)
	assert.Equal(t, in.LastName, got.LastName)
	assert.Equal(t, in.NickName, got.NickName)
	assert.Equal(t, in.Description, got.Description)
	assert.Equal(t, in.AvatarUrl, got.AvatarUrl)
	assert.Equal(t, in.Location, got.Location)
	assert.Equal(t, in.ExpiresAt, got.ExpiresAt)
}

// The response contract is "same shape, credentials nulled". If someone adds a
// column to socials and regenerates, repository.Social grows a key that
// socialResponse lacks and the response silently loses a field. Comparing key
// sets catches that at test time rather than in a client bug report.
func TestSocialResponse_KeySetMatchesRepositorySocial(t *testing.T) {
	keysOf := func(v any) map[string]struct{} {
		raw, err := json.Marshal(v)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(raw, &m))

		out := make(map[string]struct{}, len(m))
		for k := range m {
			out[k] = struct{}{}
		}
		return out
	}

	assert.Equal(
		t,
		keysOf(repository.Social{}),
		keysOf(socialResponse{}),
		"socialResponse must mirror repository.Social's JSON keys exactly — "+
			"add the new field here (or null it, if it is a credential)",
	)
}

func TestSanitizeSocials_EmptyIsArrayNotNull(t *testing.T) {
	raw, err := json.Marshal(sanitizeSocials(nil))
	require.NoError(t, err)
	assert.Equal(t, "[]", string(raw))
}
