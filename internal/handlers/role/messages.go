package role

// Frozen wire strings: the exact error bodies this handler emitted before the
// service extraction. They are part of the live contract until a versioned
// error format is agreed, so they must not be reworded.
//
// Near-duplicates are kept apart deliberately. msgRetry and msgRetryLater
// differ by one word and both ship today, on different endpoints; collapsing
// them would be a silent wire change dressed up as tidying.
//
// The rule this file encodes: the service returns bare core sentinels, and the
// handler owns every user-facing string. That keeps the service transport
// agnostic and testable on errors.Is, and keeps all the frozen wording in one
// greppable place to delete when a v2 error contract lands.
const (
	msgGeneric      = "We ran into a problem while servicing your request please try again later"
	msgCheckBody    = "Please check your request body and try again"
	msgRetry        = "We couldn't complete this request at the moment please try again"
	msgRetryLater   = "We couldn't complete this request at the moment please try again later"
	msgRoleNotFound = "The role you are requesting does not exist"
)
