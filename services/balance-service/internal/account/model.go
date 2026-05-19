package account

// Snapshot is the serialized account state stored on the Redis hot path.
type Snapshot struct {
	ID             string `json:"id"`
	Currency       string `json:"currency"`
	AvailableMinor int64  `json:"available_minor"`
	PostedMinor    int64  `json:"posted_minor"`
	AccountStatus  string `json:"account_status"`
}

// AuthorizationRequest captures the account state needed to place a hold.
type AuthorizationRequest struct {
	AccountID string
	TxnID     string
	Currency  string
	MinorUnit int64
}

// AuthorizationResult summarizes the outcome of an authorize call.
type AuthorizationResult struct {
	Approved   bool
	ReasonCode string
	HoldID     string
	Snapshot   *Snapshot
	CacheHit   bool
}

// MutationResult represents the outcome of a release or capture mutation.
type MutationResult struct {
	HoldID   string
	Snapshot *Snapshot
}
