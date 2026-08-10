package servicetoken

// Frozen wire strings: the exact error bodies this handler emitted before the
// service extraction. Part of the live contract until a versioned error
// format is agreed, so they must not be reworded.
//
// This handler reported every error as plain text via http.Error -- unlike
// every other migrated handler, which at least JSON-wrapped its bodies. Per
// an explicit decision, these now go through core.WriteError like the rest
// of the API: same status codes and message text, wrapped as
// {"error":"..."} under application/json instead of a bare string under
// text/plain. That is a real, deliberate body-shape change on this handler
// specifically, not the header-only change ADR 0009 covers for the others.
const (
	msgUnauthorized      = "unauthorized: missing claims"
	msgInvalidToken      = "Invalid token"
	msgInternalError     = "Internal server error"
	msgAccountNotFound   = "Account not found"
	msgNotBotAccount     = "Only bot accounts can create service tokens"
	msgInvalidBody       = "Invalid request body"
	msgInvalidRotation   = "Invalid rotation policy"
	msgInvalidMetadata   = "Invalid metadata"
	msgCreateFailed      = "Failed to create service token"
	msgGenerateFailed    = "Failed to generate token"
	msgListFailed        = "Failed to list service tokens"
	msgInvalidTokenID    = "Invalid token ID"
	msgTokenNotFound     = "Service token not found"
	msgAccessDenied      = "Access denied"
	msgUpdateFailed      = "Failed to update service token"
	msgRetrieveFailed    = "Failed to retrieve updated token"
	msgGenerateNewFailed = "Failed to generate new token"
	msgRotateFailed      = "Failed to rotate service token"
	msgRevokeFailed      = "Failed to revoke service token"
	msgStatsFailed       = "Failed to get service token stats"
	msgCleanupFailed     = "Failed to cleanup expired tokens"
)
