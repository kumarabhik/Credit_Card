package account

// Snapshot is the serialized account state stored on the Redis hot path.
type Snapshot struct {
	ID             string `json:"id"`
	Currency       string `json:"currency"`
	AvailableMinor int64  `json:"available_minor"`
	PostedMinor    int64  `json:"posted_minor"`
	AccountStatus  string `json:"account_status"`
}
