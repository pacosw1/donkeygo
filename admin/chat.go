package admin

import "net/http"

// ChatTab creates the built-in Chat tab with real-time WebSocket threads.
// Delegates entirely to the ChatProvider (chat.Service admin methods).
func ChatTab(chatSvc ChatProvider) Tab {
	return Tab{
		ID:      "chat",
		Label:   "Chat",
		Icon:    "chat",
		Handler: &chatHandler{svc: chatSvc},
		Order:   70,
	}
}

type chatHandler struct {
	svc ChatProvider
}

func (h *chatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "chat.html", nil)
}
