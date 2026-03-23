package postgres

import (
	"fmt"

	"github.com/pacosw1/donkeygo/chat"
)

// ── chat.ChatDB ─────────────────────────────────────────────────────────────

// GetChatMessages returns paginated chat messages for a user, ordered by creation time descending.
func (d *DB) GetChatMessages(userID string, limit, offset int) ([]*chat.ChatMessage, error) {
	rows, err := d.db.Query(`
		SELECT id, user_id, sender, message, message_type, read_at, created_at
		FROM chat_messages
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("get chat messages: %w", err)
	}
	defer rows.Close()

	var msgs []*chat.ChatMessage
	for rows.Next() {
		m := &chat.ChatMessage{}
		if err := rows.Scan(&m.ID, &m.UserID, &m.Sender, &m.Message, &m.MessageType, &m.ReadAt, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// GetChatMessagesSince returns chat messages for a user with an ID greater than sinceID.
func (d *DB) GetChatMessagesSince(userID string, sinceID int) ([]*chat.ChatMessage, error) {
	rows, err := d.db.Query(`
		SELECT id, user_id, sender, message, message_type, read_at, created_at
		FROM chat_messages
		WHERE user_id = $1 AND id > $2
		ORDER BY created_at ASC`,
		userID, sinceID,
	)
	if err != nil {
		return nil, fmt.Errorf("get chat messages since: %w", err)
	}
	defer rows.Close()

	var msgs []*chat.ChatMessage
	for rows.Next() {
		m := &chat.ChatMessage{}
		if err := rows.Scan(&m.ID, &m.UserID, &m.Sender, &m.Message, &m.MessageType, &m.ReadAt, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// SendChatMessage inserts a new chat message and returns the created record.
func (d *DB) SendChatMessage(userID, sender, message, messageType string) (*chat.ChatMessage, error) {
	m := &chat.ChatMessage{}
	err := d.db.QueryRow(`
		INSERT INTO chat_messages (user_id, sender, message, message_type)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, sender, message, message_type, read_at, created_at`,
		userID, sender, message, messageType,
	).Scan(&m.ID, &m.UserID, &m.Sender, &m.Message, &m.MessageType, &m.ReadAt, &m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("send chat message: %w", err)
	}
	return m, nil
}

// MarkChatRead marks all unread messages in a thread as read for the given reader.
// For example, when the user reads, messages sent by admin are marked read, and vice versa.
func (d *DB) MarkChatRead(userID, reader string) error {
	_, err := d.db.Exec(`
		UPDATE chat_messages
		SET read_at = NOW()::text
		WHERE user_id = $1 AND sender != $2 AND read_at IS NULL`,
		userID, reader,
	)
	if err != nil {
		return fmt.Errorf("mark chat read: %w", err)
	}
	return nil
}

// GetUnreadCount returns the number of unread messages from admin for a user.
func (d *DB) GetUnreadCount(userID string) (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*)
		FROM chat_messages
		WHERE user_id = $1 AND sender = 'admin' AND read_at IS NULL`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get unread count: %w", err)
	}
	return count, nil
}

// AdminListChatThreads returns a summary of all chat threads for the admin view.
func (d *DB) AdminListChatThreads(limit int) ([]*chat.ChatThread, error) {
	rows, err := d.db.Query(`
		SELECT
			cm.user_id,
			COALESCE(u.name, '') AS user_name,
			COALESCE(u.email, '') AS user_email,
			cm.last_message,
			cm.last_sender,
			COALESCE(unread.cnt, 0) AS unread_count,
			cm.last_message_at
		FROM (
			SELECT DISTINCT ON (user_id)
				user_id,
				message AS last_message,
				sender  AS last_sender,
				created_at AS last_message_at
			FROM chat_messages
			ORDER BY user_id, created_at DESC
		) cm
		LEFT JOIN users u ON u.id = cm.user_id
		LEFT JOIN (
			SELECT user_id, COUNT(*) AS cnt
			FROM chat_messages
			WHERE sender = 'user' AND read_at IS NULL
			GROUP BY user_id
		) unread ON unread.user_id = cm.user_id
		ORDER BY cm.last_message_at DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("admin list chat threads: %w", err)
	}
	defer rows.Close()

	var threads []*chat.ChatThread
	for rows.Next() {
		t := &chat.ChatThread{}
		if err := rows.Scan(&t.UserID, &t.UserName, &t.UserEmail, &t.LastMessage, &t.LastSender, &t.UnreadCount, &t.LastMessageAt); err != nil {
			return nil, fmt.Errorf("scan chat thread: %w", err)
		}
		threads = append(threads, t)
	}
	return threads, rows.Err()
}

// ── ChatDBAdapter ───────────────────────────────────────────────────────────

// ChatDBAdapter wraps *DB to satisfy chat.ChatDB.
//
// The chat.ChatDB interface defines EnabledDeviceTokens(userID string) ([]string, error),
// which returns plain token strings for push delivery. The notify.NotifyDB interface
// defines the same method name but returns []*notify.DeviceToken. Since Go does not
// allow a single struct to implement two interfaces with the same method name but
// different return types, this adapter bridges the gap.
//
// All other chat.ChatDB methods delegate directly to *DB.
type ChatDBAdapter struct {
	*DB
}

// EnabledDeviceTokens returns enabled push token strings for a user, satisfying chat.ChatDB.
func (a *ChatDBAdapter) EnabledDeviceTokens(userID string) ([]string, error) {
	rows, err := a.db.Query(`
		SELECT token
		FROM user_device_tokens
		WHERE user_id = $1 AND enabled = true`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("enabled device tokens (chat): %w", err)
	}
	defer rows.Close()

	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, fmt.Errorf("scan device token: %w", err)
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}
