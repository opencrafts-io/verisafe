package streak

// Frozen wire strings: the exact error bodies this handler emitted before the
// service extraction. Part of the live contract until a versioned error format
// is agreed, so they must not be reworded.
const (
	msgCheckBody       = "Please check your request body and try again"
	msgOwnAccountOnly  = "you can only record activity completions for your own account"
	msgInternalServer  = "internal server error"
	msgCannotProcess   = "Cannot process your request at the moment"
	msgGeneric         = "We ran into a problem while servicing your request please try again later"
	msgActiveCountFail = "We couldn't fetch active streak milestone count at the moment"
	msgActiveListFail  = "We couldn't provide active streak milestones at the moment"
)
