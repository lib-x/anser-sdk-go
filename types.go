package anser

import "encoding/json"

type MessageFormat string

const (
	Text     MessageFormat = "text"
	Markdown MessageFormat = "markdown"
	HTML     MessageFormat = "html"
)

type Attachment struct {
	URL         string `json:"url"`
	Name        string `json:"name,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

type Message struct {
	Title       string          `json:"title,omitempty"`
	Body        string          `json:"body,omitempty"`
	Format      MessageFormat   `json:"format,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	URL         string          `json:"url,omitempty"`
	Level       string          `json:"level,omitempty"`
	Attachments []Attachment    `json:"attachments,omitempty"`
	Extras      json.RawMessage `json:"extras,omitempty"`
}

type Target struct {
	Channel     string   `json:"channel,omitempty"`
	AccountID   string   `json:"account_id,omitempty"`
	TagSelector []string `json:"tag_selector,omitempty"`
	Weight      *uint32  `json:"weight,omitempty"`
	Priority    *int32   `json:"priority,omitempty"`
}

type Retry struct {
	MaxAttempts  uint32 `json:"max_attempts,omitempty"`
	Backoff      string `json:"backoff,omitempty"`
	BaseDelay    uint64 `json:"base_delay,omitempty"`
	MaxDelay     uint64 `json:"max_delay,omitempty"`
	Jitter       *bool  `json:"jitter,omitempty"`
	UntilSuccess bool   `json:"until_success,omitempty"`
	TTL          uint64 `json:"ttl,omitempty"`
}

type Strategy struct {
	// Retry durations are in milliseconds.
	Retry *Retry `json:"retry,omitempty"`
	// Mode is broadcast, failover, weighted, or weighted_failover.
	Mode string `json:"mode,omitempty"`
	// SuccessCriteria is any or all.
	SuccessCriteria string `json:"success_criteria,omitempty"`
}

type Publish struct {
	Message
	// Channels accepts account IDs or channel names. Use Targets for selectors.
	// Set either Channels or Targets, or leave both empty for server routing.
	Channels []string  `json:"-"`
	Targets  []Target  `json:"-"`
	Strategy *Strategy `json:"strategy,omitempty"`
	// ExpireAt is a UTC Unix timestamp in milliseconds.
	ExpireAt      int64  `json:"expire_at,omitempty"`
	CallbackURL   string `json:"callback_url,omitempty"`
	TopicPassword string `json:"topic_password,omitempty"`
}

type Receipt struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

type SubscriptionInput struct {
	SubscriberRef string          `json:"subscriber_ref"`
	AccountID     string          `json:"account_id,omitempty"`
	Target        json.RawMessage `json:"target,omitempty"`
	Password      string          `json:"password,omitempty"`
	Filter        []string        `json:"filter,omitempty"`
}

type Subscription struct {
	UserID        *string  `json:"user_id"`
	ID            string   `json:"id"`
	SubscriberRef string   `json:"subscriber_ref"`
	AccountID     *string  `json:"account_id"`
	Filter        []string `json:"filter"`
	CreatedAt     int64    `json:"created_at"`
}

type Attempt struct {
	Number       int64   `json:"n"`
	StartedAt    int64   `json:"started_at"`
	FinishedAt   *int64  `json:"finished_at"`
	ErrorCode    *string `json:"error_code"`
	ErrorMessage *string `json:"error_msg"`
	Latency      *int64  `json:"latency"`
}

type Delivery struct {
	ID             string    `json:"id"`
	Channel        string    `json:"channel"`
	AccountID      string    `json:"account_id"`
	AccountName    *string   `json:"account_name"`
	SubscriptionID *string   `json:"subscription_id"`
	Status         string    `json:"status"`
	NextRetryAt    *int64    `json:"next_retry_at"`
	Attempts       []Attempt `json:"attempts"`
}

type MessageDetail struct {
	ID              string     `json:"id"`
	Topic           *string    `json:"topic"`
	Status          string     `json:"status"`
	Message         Message    `json:"message"`
	CreatedAt       int64      `json:"created_at"`
	InboxDeliveries int64      `json:"inbox_deliveries"`
	InboxRecipient  *string    `json:"inbox_recipient"`
	Deliveries      []Delivery `json:"deliveries"`
}
