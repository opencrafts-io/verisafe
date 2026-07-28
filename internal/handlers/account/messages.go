package account

// Frozen wire strings: the exact error bodies this handler emitted before the
// service extraction. Part of the live contract until a versioned error
// format is agreed, so they must not be reworded -- including one real quirk
// kept exactly as it shipped: UpdatePersonalAccount and VerifyPhone's
// ownership-mismatch check returns 500, not 403.
const (
	msgGeneric             = "We ran into a problem while servicing your request please try again later"
	msgAuthRequired        = "authentication required"
	msgInvalidBody         = "Invalid request body"
	msgEmailNameRequired   = "Email and name are required"
	msgTokenNameRequired   = "Service token name is required"
	msgAccountCreateFailed = "We couldn't create this account at the moment please try again later"
	msgRoleLookupFailed    = "We couldn't found a suitable role to assign to your bot"
	msgRoleAssignFailed    = "We couldn't assign default bot role"
	msgTokenGenFailed      = "Failed to generate service token"
	msgInvalidRotation     = "Invalid rotation policy"
	msgInvalidMetadata     = "Invalid metadata"
	msgServiceTokenFailed  = "Failed to create service token"

	msgFanoutServiceUnavailable = "We ran into an error while trying to service your request"
	msgSomeBatchesFailed        = "Some batches failed to publish"

	msgFetchAccountFailed = "We ran into an error while trying to fetch your account"
	msgWrongFlavor        = "Account does not exist your token might be from a different flavor"

	msgCheckBody           = "Please check your request body and try again"
	msgOwnershipViolation  = "You dont have permissions to update this account"
	msgUpdateAccountFailed = "We ran into an error while trying to update your account"

	msgSearchQueryRequired = "Search query parameter 'q' is required"
	msgSearchFailed        = "We couldn't complete this request at the moment please try again later"

	msgDeletionBeginFailed = "We ran into an error while trying to delete your account"
	msgDeletionFailed      = "We couldn't delete your account at the moment please try again later"
	msgRecoveryBeginFailed = "We ran into an error while trying to recover your account"
	msgRecoveryFailed      = "We couldn't recover your account at the moment please try again later"
)
