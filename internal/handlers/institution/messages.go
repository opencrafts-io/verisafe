package institution

// Frozen wire strings: the exact error bodies this handler emitted before the
// service extraction. Part of the live contract until a versioned error
// format is agreed, so they must not be reworded -- including the two
// pre-existing wording bugs kept exactly as they shipped: RemoveAccountInstitution's
// repo-call failure says "failed to create institution" (copy-pasted from a
// create path, never corrected), and there is no dedicated "unlink" message.
const (
	msgInternalServer     = "internal server error"
	msgInvalidBody        = "invalid request body"
	msgInvalidID          = "invalid institution id"
	msgCreateFailed       = "failed to create institution"
	msgUpdateFailed       = "failed to update institution"
	msgNotFound           = "institution not found"
	msgFetchFailed        = "failed to fetch institutions"
	msgDeleteFailed       = "failed to delete institution"
	msgMissingQuery       = "missing search query param 'q'"
	msgSearchFailed       = "failed to search institutions"
	msgOwnMembership      = "you can only manage your own institution memberships"
	msgLinkFailed         = "failed to link you to that organization"
	msgInvalidUUIDParam   = "Could not parse the uuid parameter"
	msgInvalidInstIDParam = "Could not parse the institution id parameter"
)
