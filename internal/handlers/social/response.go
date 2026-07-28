package social

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// socialResponse is the wire form of a linked social account.
//
// It exists because the handlers used to serialize repository.Social directly,
// which meant GET /socials/me handed a user their own provider access and
// refresh tokens, and GET /socials/user/{user_id} handed any holder of
// read:account:any every user's. Those are long-lived, replayable third-party
// credentials; they have no business crossing an API boundary.
//
// The three credential fields are retained as always-null so the response
// shape is unchanged for existing clients — a field that disappears breaks a
// strict decoder, a field that turns null does not. They are dropped once no
// client references them.
//
// Provider tokens are reachable only through the broker
// (POST /oauth/{provider}/token), which requires a service token and a
// specific permission, and never returns a refresh token at all.
type socialResponse struct {
	UserID      string    `json:"user_id"`
	IDToken     *string   `json:"id_token"`
	AccountID   uuid.UUID `json:"account_id"`
	Provider    string    `json:"provider"`
	Email       *string   `json:"email"`
	Name        *string   `json:"name"`
	FirstName   *string   `json:"first_name"`
	LastName    *string   `json:"last_name"`
	NickName    *string   `json:"nick_name"`
	Description *string   `json:"description"`
	AvatarUrl   *string   `json:"avatar_url"`
	Location    *string   `json:"location"`

	// Always null. See the type comment.
	AccessToken       *string `json:"access_token"`
	AccessTokenSecret *string `json:"access_token_secret"`
	RefreshToken      *string `json:"refresh_token"`

	ExpiresAt pgtype.Timestamp `json:"expires_at"`
	CreatedAt pgtype.Timestamp `json:"created_at"`
	UpdatedAt pgtype.Timestamp `json:"updated_at"`
}

// sanitizeSocial strips the credential fields from a single row.
func sanitizeSocial(s repository.Social) socialResponse {
	return socialResponse{
		UserID:      s.UserID,
		IDToken:     s.IDToken,
		AccountID:   s.AccountID,
		Provider:    s.Provider,
		Email:       s.Email,
		Name:        s.Name,
		FirstName:   s.FirstName,
		LastName:    s.LastName,
		NickName:    s.NickName,
		Description: s.Description,
		AvatarUrl:   s.AvatarUrl,
		Location:    s.Location,
		ExpiresAt:   s.ExpiresAt,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
		// AccessToken, AccessTokenSecret and RefreshToken are deliberately
		// left at their zero value.
	}
}

// sanitizeSocials strips credentials from a result set. Returns an empty
// slice rather than nil so the JSON is [] rather than null.
func sanitizeSocials(rows []repository.Social) []socialResponse {
	out := make([]socialResponse, 0, len(rows))
	for _, s := range rows {
		out = append(out, sanitizeSocial(s))
	}
	return out
}
