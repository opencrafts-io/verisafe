package social

// Frozen wire strings: the exact error bodies this handler emitted before the
// service extraction. Part of the live contract until a versioned error format
// is agreed, so they must not be reworded.
const (
	msgGeneric      = "We ran into a problem while servicing your request please try again later"
	msgCheckBody    = "Please check your request body and try again"
	msgFetchFailed  = "We couldn't fetch your social login providers at the moment please try again"
	msgAuthRequired = "authentication required"
	msgCheckToken   = "Please check your request auth token and try again"
)
