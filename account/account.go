// Package account provides GDPR-compliant account deletion, anonymization, and data export.
//
// Usage:
//
//	accountSvc := account.New(account.Config{
//	    OnDelete: func(userID, email string) {
//	        emailProvider.Send(email, "Account Deleted", "Your account has been deleted.", "")
//	    },
//	}, store, &myAppCleanup{})
//
//	mux.HandleFunc("DELETE /api/v1/account", requireAuth(accountSvc.HandleDeleteAccount))
//	mux.HandleFunc("POST /api/v1/account/anonymize", requireAuth(accountSvc.HandleAnonymizeAccount))
//	mux.HandleFunc("GET /api/v1/account/export", requireAuth(accountSvc.HandleExportData))
package account

import (
	"net/http"

	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
)

// AccountDB handles donkeygo-internal table cleanup.
type AccountDB interface {
	// GetUserEmail returns the user's email (for OnDelete callback).
	GetUserEmail(userID string) (string, error)
	// DeleteUserData removes all user data from donkeygo-managed tables:
	// user_subscriptions, user_activity, user_feedback, user_sessions,
	// user_device_tokens, user_notification_preferences, notification_deliveries,
	// chat_messages, tombstones, verified_transactions, feature_flag_overrides.
	DeleteUserData(userID string) error
	// DeleteUser removes the user from the users table.
	DeleteUser(userID string) error
	// AnonymizeUser replaces PII (email, name) with anonymized values.
	AnonymizeUser(userID string) error
	// ExportUserData returns all user data as a structured export.
	ExportUserData(userID string) (*UserDataExport, error)
}

// AppCleanup is an optional interface apps implement to clean up their own tables.
type AppCleanup interface {
	DeleteAppData(userID string) error
}

// AppExporter is an optional interface apps implement to include app data in exports.
type AppExporter interface {
	ExportAppData(userID string) (any, error)
}

// UserDataExport holds all exportable user data.
type UserDataExport struct {
	User         any `json:"user"`
	Subscription any `json:"subscription,omitempty"`
	Events       any `json:"events,omitempty"`
	Sessions     any `json:"sessions,omitempty"`
	Feedback     any `json:"feedback,omitempty"`
	ChatMessages any `json:"chat_messages,omitempty"`
	DeviceTokens any `json:"device_tokens,omitempty"`
	Preferences  any `json:"notification_preferences,omitempty"`
	Transactions any `json:"transactions,omitempty"`
	AppData      any `json:"app_data,omitempty"`
}

// Config holds account management configuration.
type Config struct {
	// OnDelete is called after successful account deletion with the user's ID and email.
	OnDelete func(userID, email string)
}

// Service provides account management handlers.
type Service struct {
	cfg        Config
	db         AccountDB
	appCleanup AppCleanup
	appExport  AppExporter
}

// New creates an account management service.
// Optional AppCleanup and AppExporter interfaces can be passed to handle app-specific data.
func New(cfg Config, db AccountDB, opts ...any) *Service {
	s := &Service{cfg: cfg, db: db}
	for _, opt := range opts {
		if ac, ok := opt.(AppCleanup); ok {
			s.appCleanup = ac
		}
		if ae, ok := opt.(AppExporter); ok {
			s.appExport = ae
		}
	}
	return s
}

// HandleDeleteAccount handles DELETE /api/v1/account.
// Permanently deletes all user data across all donkeygo tables and app tables.
func (s *Service) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	// Get email before deletion for callback
	email, _ := s.db.GetUserEmail(userID)

	// 1. App-specific tables first (may have FKs to users)
	if s.appCleanup != nil {
		if err := s.appCleanup.DeleteAppData(userID); err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "failed to delete app data")
			return
		}
	}

	// 2. All donkeygo-managed tables (except users)
	if err := s.db.DeleteUserData(userID); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to delete user data")
		return
	}

	// 3. Delete user record last
	if err := s.db.DeleteUser(userID); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}

	// 4. Callback
	if s.cfg.OnDelete != nil && email != "" {
		s.cfg.OnDelete(userID, email)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// HandleAnonymizeAccount handles POST /api/v1/account/anonymize.
// Replaces PII with anonymized values but keeps analytics data intact.
func (s *Service) HandleAnonymizeAccount(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	if err := s.db.AnonymizeUser(userID); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to anonymize account")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "anonymized"})
}

// HandleExportData handles GET /api/v1/account/export.
// Returns all user data as JSON (GDPR data portability).
func (s *Service) HandleExportData(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	export, err := s.db.ExportUserData(userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to export data")
		return
	}

	// Include app data if exporter is available
	if s.appExport != nil {
		appData, err := s.appExport.ExportAppData(userID)
		if err == nil {
			export.AppData = appData
		}
	}

	w.Header().Set("Content-Disposition", "attachment; filename=account-data.json")
	httputil.WriteJSON(w, http.StatusOK, export)
}
