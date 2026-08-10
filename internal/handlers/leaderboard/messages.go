package leaderboard

// Frozen wire strings: the exact error bodies this handler emitted before the
// service extraction. Part of the live contract until a versioned error format
// is agreed, so they must not be reworded.
//
// msgInternalServer keeps its original, unrelated-looking wording
// ("internal server error", no capital, no trailing punctuation) rather than
// being folded into msgLeaderboardFailed or the msgGeneric string other
// handlers use -- it is a distinct string this endpoint already shipped.
const (
	msgInternalServer    = "internal server error"
	msgCannotProcess     = "Cannot process your request at the moment"
	msgInvalidUserID     = "invalid user id"
	msgLeaderboardFailed = "We couldn't provide the global leaderboard at the moment"
)
