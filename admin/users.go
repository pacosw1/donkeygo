package admin

import (
	"net/http"
	"strconv"
)

// UsersConfig configures the built-in Users tab.
type UsersConfig struct {
	ExtraColumns  []Column  // Additional columns in the user list table
	ExtraSections []Section // Additional sections in the user detail view
}

// UsersTab creates the built-in Users tab with search, pagination, and detail view.
func UsersTab(db AdminDB, cfg ...UsersConfig) Tab {
	var c UsersConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return Tab{
		ID:      "users",
		Label:   "Users",
		Icon:    "users",
		Handler: &usersHandler{db: db, cfg: c},
		Order:   20,
	}
}

type usersHandler struct {
	db  AdminDB
	cfg UsersConfig
}

type usersData struct {
	Users        []userRow
	Search       string
	Page         int
	TotalPages   int
	Pages        []int
	ExtraHeaders []string
}

type userRow struct {
	AdminUser
	ExtraCells []string
}

type userDetailData struct {
	User          AdminUserDetail
	ExtraSections []renderedSection
}

type renderedSection struct {
	Title string
	HTML  string
}

func (h *usersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if this is a detail request
	userID := r.URL.Query().Get("id")
	if userID != "" {
		h.serveDetail(w, r, userID)
		return
	}

	search := r.URL.Query().Get("search")
	page := intQueryParam(r, "page", 1)
	limit := 50
	offset := (page - 1) * limit

	users, total, err := h.db.AdminListUsers(search, limit, offset)
	if err != nil {
		http.Error(w, "failed to list users", http.StatusInternalServerError)
		return
	}

	// Build rows with extra columns
	rows := make([]userRow, len(users))
	for i, u := range users {
		cells := make([]string, len(h.cfg.ExtraColumns))
		for j, col := range h.cfg.ExtraColumns {
			cells[j] = col.Value(u)
		}
		rows[i] = userRow{AdminUser: u, ExtraCells: cells}
	}

	headers := make([]string, len(h.cfg.ExtraColumns))
	for i, col := range h.cfg.ExtraColumns {
		headers[i] = col.Header
	}

	totalPages := (total + limit - 1) / limit
	pages := make([]int, totalPages)
	for i := range pages {
		pages[i] = i + 1
	}

	renderTemplate(w, "users.html", usersData{
		Users:        rows,
		Search:       search,
		Page:         page,
		TotalPages:   totalPages,
		Pages:        pages,
		ExtraHeaders: headers,
	})
}

func (h *usersHandler) serveDetail(w http.ResponseWriter, r *http.Request, userID string) {
	detail, err := h.db.AdminGetUser(userID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	// Render extra sections
	sections := make([]renderedSection, len(h.cfg.ExtraSections))
	for i, s := range h.cfg.ExtraSections {
		sections[i] = renderedSection{
			Title: s.Title,
			HTML:  renderHandlerToString(s.Handler, r),
		}
	}

	renderTemplate(w, "user_detail.html", userDetailData{
		User:          *detail,
		ExtraSections: sections,
	})
}

func intQueryParam(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}
