package admin

import "time"

// AdminDB provides admin-specific queries that span multiple tables.
// Implement this interface in your database layer (e.g. postgres.AdminDBAdapter).
type AdminDB interface {
	// Users
	AdminListUsers(search string, limit, offset int) ([]AdminUser, int, error)
	AdminGetUser(userID string) (*AdminUserDetail, error)

	// Events
	AdminListEvents(eventType, userID string, since time.Time, limit int) ([]AdminEvent, error)

	// Notifications
	AdminListNotifications(limit int) ([]AdminNotification, error)

	// Subscriptions
	AdminSubscriptionBreakdown() ([]SubBreakdownRow, error)

	// Feedback
	AdminListFeedback(limit int) ([]AdminFeedback, error)
}

// AdminUser is a user row for the admin list view.
type AdminUser struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
	Status      string    `json:"status"` // "active", "pro", "free", etc.
}

// AdminUserDetail is the full user profile for the detail view.
type AdminUserDetail struct {
	AdminUser
	Subscription *SubInfo `json:"subscription,omitempty"`
	EventCount   int      `json:"event_count"`
	SessionCount int      `json:"session_count"`
	DeviceCount  int      `json:"device_count"`
}

// SubInfo holds subscription details for a user.
type SubInfo struct {
	ProductID string     `json:"product_id"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// AdminEvent is an event row for the events tab.
type AdminEvent struct {
	UserID    string    `json:"user_id"`
	UserEmail string    `json:"user_email"`
	Event     string    `json:"event"`
	Metadata  string    `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
}

// AdminNotification is a notification delivery record.
type AdminNotification struct {
	UserID string    `json:"user_id"`
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	Status string    `json:"status"`
	SentAt time.Time `json:"sent_at"`
}

// SubBreakdownRow is a subscription status count.
type SubBreakdownRow struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// AdminFeedback is a feedback submission.
type AdminFeedback struct {
	UserID     string    `json:"user_id"`
	UserEmail  string    `json:"user_email"`
	Type       string    `json:"type"`
	Message    string    `json:"message"`
	AppVersion string    `json:"app_version"`
	CreatedAt  time.Time `json:"created_at"`
}
