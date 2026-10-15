package streak

// Frozen wire strings: the exact error bodies this handler emitted before the
// service extraction. Part of the live contract until a versioned error format
// is agreed, so they must not be reworded.
const (
	msgCheckBody                = "Please check your request body and try again"
	msgOwnAccountOnly           = "you can only record activity completions for your own account"
	msgInternalServer           = "internal server error"
	msgCannotProcess            = "Cannot process your request at the moment"
	msgGeneric                  = "We ran into a problem while servicing your request please try again later"
	msgActiveCountFail          = "We couldn't fetch active streak milestone count at the moment"
	msgActiveListFail           = "We couldn't provide active streak milestones at the moment"
	msgServiceTokenOnly         = "this operation requires a service token"
	msgInvalidRewardRequest     = "account_id, activity_id, and idempotency_key (1-200 characters) are required"
	msgInvalidCompletionRequest = "account_id and activity_id are required, metadata must be valid JSON, and idempotency_key cannot exceed 200 characters"
	msgInvalidMilestone         = "activity_id and title are required; days_required must be positive and bonus_points must be between 1 and 10"
)
