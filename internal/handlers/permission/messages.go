package permission

// Frozen wire strings: the exact error bodies this handler emitted before the
// service extraction. Part of the live contract until a versioned error format
// is agreed, so they must not be reworded.
//
// As in the role handler, near-duplicates are kept apart deliberately:
// msgRetry and msgRetryLater differ by one word and both ship today, on
// different endpoints.
const (
	msgGeneric            = "We ran into a problem while servicing your request please try again later"
	msgCheckBody          = "Please check your request body and try again"
	msgRetry              = "We couldn't complete this request at the moment please try again"
	msgRetryLater         = "We couldn't complete this request at the moment please try again later"
	msgPermissionNotFound = "The permission you are requesting does not exist"
	msgCreateFailed       = "We couldn't create this permission at the moment please try again later"
	msgUserPermsFailed    = "We ran into an issue while retrieving this user's permissions try again later"
)
