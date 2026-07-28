package activity

// Frozen wire strings: the exact error bodies this handler emitted before the
// service extraction. Part of the live contract until a versioned error format
// is agreed, so they must not be reworded -- including the inconsistent
// trailing periods and the one "try that again" instead of "try again".
const (
	msgInternalServer  = "internal server error"
	msgCannotProcess   = "Cannot process your request at the moment"
	msgGeneric         = "We ran into a problem while servicing your request please try again later"
	msgCheckBody       = "Please check your request body and try again"
	msgCheckBodyRetry  = "Please check your request body and try that again"
	msgCompletionsFail = "We couldn't provide all activity completions for that user at the moment."
	msgAllCountFail    = "We couldn't provide all activities at the moment."
	msgActivitiesFail  = "We couldn't provide activities at the moment."
)
