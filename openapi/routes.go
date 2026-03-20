package openapi

// ── Helper constructors ─────────────────────────────────────────────────────

func intPtr(n int) *int { return &n }

// Str creates a string schema.
func Str(desc string) Schema { return Schema{Type: "string", Description: desc} }

// StrFmt creates a string schema with format.
func StrFmt(desc, format string) Schema { return Schema{Type: "string", Format: format, Description: desc} }

// Int creates an integer schema.
func Int(desc string) Schema { return Schema{Type: "integer", Description: desc} }

// IntRange creates an integer schema with min/max.
func IntRange(desc string, min, max int) Schema {
	return Schema{Type: "integer", Description: desc, Minimum: &min, Maximum: &max}
}

// Bool creates a boolean schema.
func Bool(desc string) Schema { return Schema{Type: "boolean", Description: desc} }

// Ref creates a $ref schema.
func Ref(name string) Schema { return Schema{Ref: name} }

// Arr creates an array schema.
func Arr(items Schema) Schema { return Schema{Type: "array", Items: &items} }

// Obj creates an object schema.
func Obj(props map[string]Schema, required ...string) Schema {
	return Schema{Type: "object", Properties: props, Required: required}
}

// StrEnum creates a string enum schema.
func StrEnum(desc string, values ...string) Schema {
	return Schema{Type: "string", Description: desc, Enum: values}
}

// NullStr creates a nullable string schema.
func NullStr(desc string) Schema {
	return Schema{Type: "string", Description: desc, Nullable: true}
}

// NullStrFmt creates a nullable string schema with format.
func NullStrFmt(desc, format string) Schema {
	return Schema{Type: "string", Format: format, Description: desc, Nullable: true}
}

// ── Package Route Exports ───────────────────────────────────────────────────

// AuthRoutes returns OpenAPI routes for the auth package.
func AuthRoutes() []Route {
	return []Route{
		{
			Method: "POST", Path: "/api/v1/auth/apple",
			Summary: "Sign in with Apple", Tags: []string{"auth"}, Auth: false,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"identity_token": Str("Apple identity token"),
				"name":           Str("User display name"),
				"platform":       StrEnum("Client platform", "ios", "web"),
			}, "identity_token")},
			Response: &Response{Status: 200, Description: "Authenticated", Schema: &Schema{Ref: "AuthResponse"}},
		},
		{
			Method: "GET", Path: "/api/v1/auth/me",
			Summary: "Get current user", Tags: []string{"auth"}, Auth: true,
			Response: &Response{Status: 200, Description: "Current user", Schema: &Schema{Ref: "User"}},
		},
		{
			Method: "POST", Path: "/api/v1/auth/logout",
			Summary: "Sign out (clears session cookie)", Tags: []string{"auth"}, Auth: true,
			Response: &Response{Status: 200, Description: "Logged out"},
		},
	}
}

// AuthSchemas returns OpenAPI schemas for the auth package.
func AuthSchemas() []ComponentSchema {
	return []ComponentSchema{
		{"User", Obj(map[string]Schema{
			"id":            Str(""),
			"apple_sub":     Str(""),
			"email":         Str(""),
			"name":          Str(""),
			"created_at":    StrFmt("", "date-time"),
			"last_login_at": StrFmt("", "date-time"),
		})},
		{"AuthResponse", Obj(map[string]Schema{
			"token": Str("JWT session token (7-day expiry)"),
			"user":  Ref("User"),
		})},
	}
}

// NotifyRoutes returns OpenAPI routes for the notify package.
func NotifyRoutes() []Route {
	return []Route{
		{
			Method: "POST", Path: "/api/v1/notifications/devices",
			Summary: "Register device for push notifications", Tags: []string{"notifications"}, Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"token":        Str("APNs device token"),
				"platform":     StrEnum("Platform", "ios", "macos", "web"),
				"device_model": Str("Device model"),
				"os_version":   Str("OS version"),
				"app_version":  Str("App version"),
			}, "token")},
			Response: &Response{Status: 201, Description: "Device registered"},
		},
		{
			Method: "DELETE", Path: "/api/v1/notifications/devices",
			Summary: "Disable device token", Tags: []string{"notifications"}, Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"token": Str("Token to disable"),
			}, "token")},
			Response: &Response{Status: 200, Description: "Device disabled"},
		},
		{
			Method: "GET", Path: "/api/v1/notifications/preferences",
			Summary: "Get notification preferences", Tags: []string{"notifications"}, Auth: true,
			Response: &Response{Status: 200, Description: "Preferences", Schema: &Schema{Ref: "NotificationPreferences"}},
		},
		{
			Method: "PUT", Path: "/api/v1/notifications/preferences",
			Summary: "Update notification preferences", Tags: []string{"notifications"}, Auth: true,
			Request: &RequestBody{Schema: Ref("NotificationPreferences")},
			Response: &Response{Status: 200, Description: "Updated preferences", Schema: &Schema{Ref: "NotificationPreferences"}},
		},
		{
			Method: "POST", Path: "/api/v1/notifications/opened",
			Summary: "Track notification open", Tags: []string{"notifications"}, Auth: true,
			Request: &RequestBody{Schema: Obj(map[string]Schema{
				"notification_id": Str("Notification ID"),
			})},
			Response: &Response{Status: 200, Description: "Recorded"},
		},
	}
}

// NotifySchemas returns OpenAPI schemas for the notify package.
func NotifySchemas() []ComponentSchema {
	return []ComponentSchema{
		{"NotificationPreferences", Obj(map[string]Schema{
			"user_id":          Str(""),
			"push_enabled":     Bool(""),
			"interval_seconds": IntRange("", 300, 86400),
			"wake_hour":        IntRange("", 0, 23),
			"sleep_hour":       IntRange("", 0, 23),
			"timezone":         Str(""),
			"stop_after_goal":  Bool(""),
		})},
		{"DeviceToken", Obj(map[string]Schema{
			"id":           Str(""),
			"token":        Str(""),
			"platform":     StrEnum("", "ios", "macos", "web"),
			"device_name":  Str(""),
			"app_version":  Str(""),
			"enabled":      Bool(""),
			"is_current":   Bool(""),
			"last_seen_at": StrFmt("", "date-time"),
		})},
	}
}

// EngageRoutes returns OpenAPI routes for the engage package.
func EngageRoutes() []Route {
	maxItems := 100
	return []Route{
		{
			Method: "POST", Path: "/api/v1/events",
			Summary: "Track analytics events (batched)", Tags: []string{"engagement"}, Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"events": {Type: "array", MaxItems: &maxItems, Items: &Schema{Ref: "Event"}},
			}, "events")},
			Response: &Response{Status: 200, Description: "Events tracked", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{"tracked": Int("")},
			}},
		},
		{
			Method: "PUT", Path: "/api/v1/subscription",
			Summary: "Sync subscription status from StoreKit", Tags: []string{"engagement"}, Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"product_id":              Str(""),
				"status":                  StrEnum("", "active", "expired", "cancelled", "trial", "free"),
				"expires_at":              NullStrFmt("", "date-time"),
				"original_transaction_id": Str(""),
				"price_cents":             Int(""),
				"currency_code":           Str(""),
			}, "status")},
			Response: &Response{Status: 200, Description: "Subscription updated", Schema: &Schema{Ref: "Subscription"}},
		},
		{
			Method: "POST", Path: "/api/v1/sessions",
			Summary: "Report session start/end", Tags: []string{"engagement"}, Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"session_id":  Str(""),
				"action":      StrEnum("", "start", "end"),
				"app_version": Str(""),
				"os_version":  Str(""),
				"country":     Str(""),
				"duration_s":  Int("Seconds (sent on end)"),
			}, "session_id", "action")},
			Response: &Response{Status: 200, Description: "Session recorded"},
		},
		{
			Method: "GET", Path: "/api/v1/user/eligibility",
			Summary: "Get paywall trigger and engagement data", Tags: []string{"engagement"}, Auth: true,
			Response: &Response{Status: 200, Description: "Eligibility", Schema: &Schema{Ref: "Eligibility"}},
		},
		{
			Method: "POST", Path: "/api/v1/feedback",
			Summary: "Submit user feedback", Tags: []string{"engagement"}, Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"type":        StrEnum("", "positive", "negative", "bug", "feature", "general"),
				"message":     Str("Feedback message"),
				"app_version": Str(""),
			}, "message")},
			Response: &Response{Status: 201, Description: "Feedback received"},
		},
	}
}

// EngageSchemas returns OpenAPI schemas for the engage package.
func EngageSchemas() []ComponentSchema {
	return []ComponentSchema{
		{"Event", Obj(map[string]Schema{
			"event":     Str("Event name"),
			"metadata":  {Type: "object", Description: "Arbitrary metadata"},
			"timestamp": StrFmt("", "date-time"),
		})},
		{"Subscription", Obj(map[string]Schema{
			"user_id":    Str(""),
			"product_id": Str(""),
			"status":     StrEnum("", "active", "expired", "cancelled", "trial", "free"),
			"expires_at": NullStrFmt("", "date-time"),
			"updated_at": StrFmt("", "date-time"),
		})},
		{"Eligibility", Obj(map[string]Schema{
			"paywall_trigger": NullStr("Trigger name or null"),
			"days_active":     Int(""),
			"total_logs":      Int(""),
			"streak":          Int(""),
			"is_pro":          Bool(""),
		})},
	}
}

// ChatRoutes returns OpenAPI routes for the chat package.
func ChatRoutes() []Route {
	return []Route{
		{
			Method: "GET", Path: "/api/v1/chat",
			Summary: "Get chat history", Tags: []string{"chat"}, Auth: true,
			Parameters: []Parameter{
				{Name: "limit", In: "query", Schema: Schema{Type: "integer", Default: 50}},
				{Name: "offset", In: "query", Schema: Schema{Type: "integer", Default: 0}},
				{Name: "since_id", In: "query", Description: "Return messages newer than this ID", Schema: Int("")},
			},
			Response: &Response{Status: 200, Description: "Chat messages", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{
					"messages": Arr(Ref("ChatMessage")),
					"has_more": Bool(""),
				},
			}},
		},
		{
			Method: "POST", Path: "/api/v1/chat",
			Summary: "Send chat message", Tags: []string{"chat"}, Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"message":      Str("Message text (max 5000 chars)"),
				"message_type": StrEnum("", "text", "image"),
			}, "message")},
			Response: &Response{Status: 201, Description: "Message sent"},
		},
		{
			Method: "GET", Path: "/api/v1/chat/unread",
			Summary: "Get unread message count", Tags: []string{"chat"}, Auth: true,
			Response: &Response{Status: 200, Description: "Unread count", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{"count": Int("")},
			}},
		},
		{
			Method: "POST", Path: "/api/v1/chat/upload",
			Summary: "Upload chat image", Tags: []string{"chat"}, Auth: true,
			Request: &RequestBody{ContentType: "multipart/form-data", Schema: Obj(map[string]Schema{
				"image": StrFmt("", "binary"),
			})},
			Response: &Response{Status: 200, Description: "Upload URL", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{"url": Str("")},
			}},
		},
		{
			Method: "GET", Path: "/api/v1/chat/ws",
			Summary: "WebSocket connection for real-time chat", Tags: []string{"chat"}, Auth: false,
			Parameters: []Parameter{
				{Name: "token", In: "query", Required: true, Schema: Str("Session JWT")},
			},
		},
	}
}

// ChatSchemas returns OpenAPI schemas for the chat package.
func ChatSchemas() []ComponentSchema {
	return []ComponentSchema{
		{"ChatMessage", Obj(map[string]Schema{
			"id":           Int(""),
			"user_id":      Str(""),
			"sender":       StrEnum("", "user", "admin"),
			"message":      Str(""),
			"message_type": StrEnum("", "text", "image"),
			"read_at":      NullStrFmt("", "date-time"),
			"created_at":   StrFmt("", "date-time"),
		})},
	}
}

// SyncRoutes returns OpenAPI routes for the sync package.
func SyncRoutes() []Route {
	return []Route{
		{
			Method: "GET", Path: "/api/v1/sync/changes",
			Summary: "Get changes since timestamp (delta sync)", Tags: []string{"sync"}, Auth: true,
			Parameters: []Parameter{
				{Name: "since", In: "query", Description: "ISO8601 timestamp. Omit for full sync.", Schema: StrFmt("", "date-time")},
			},
			Response: &Response{Status: 200, Description: "Sync changes", Schema: &Schema{Ref: "SyncChanges"}},
		},
		{
			Method: "POST", Path: "/api/v1/sync/batch",
			Summary: "Batch upsert entities", Tags: []string{"sync"}, Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"items": Arr(Schema{Type: "object", Description: "Entity with client_id, entity_type, and type-specific fields"}),
			}, "items")},
			Response: &Response{Status: 200, Description: "Batch results", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{
					"items":  Arr(Obj(map[string]Schema{"client_id": Str(""), "server_id": Str("")})),
					"errors": Arr(Obj(map[string]Schema{"client_id": Str(""), "error": Str("")})),
				},
			}},
		},
		{
			Method: "DELETE", Path: "/api/v1/sync/{entity_type}/{id}",
			Summary: "Delete entity and record tombstone", Tags: []string{"sync"}, Auth: true,
			Parameters: []Parameter{
				{Name: "entity_type", In: "path", Required: true, Schema: Str("")},
				{Name: "id", In: "path", Required: true, Schema: Str("")},
			},
			Response: &Response{Status: 200, Description: "Deleted"},
		},
	}
}

// SyncSchemas returns OpenAPI schemas for the sync package.
func SyncSchemas() []ComponentSchema {
	return []ComponentSchema{
		{"SyncChanges", Schema{
			Type:        "object",
			Description: "Add your app-specific entity arrays here",
			Properties: map[string]Schema{
				"deleted":   Arr(Ref("SyncDeletedEntry")),
				"synced_at": StrFmt("", "date-time"),
			},
		}},
		{"SyncDeletedEntry", Obj(map[string]Schema{
			"entity_type": Str(""),
			"entity_id":   Str(""),
			"deleted_at":  StrFmt("", "date-time"),
		})},
	}
}

// PaywallRoutes returns OpenAPI routes for the paywall package.
func PaywallRoutes() []Route {
	return []Route{
		{
			Method: "GET", Path: "/api/v1/paywall/config",
			Summary: "Get paywall content (server-driven, multi-lang)", Tags: []string{"paywall"}, Auth: false,
			Parameters: []Parameter{
				{Name: "locale", In: "query", Schema: Schema{Type: "string", Default: "en"}},
			},
			Response: &Response{Status: 200, Description: "Paywall config", Schema: &Schema{Ref: "PaywallConfig"}},
		},
	}
}

// PaywallSchemas returns OpenAPI schemas for the paywall package.
func PaywallSchemas() []ComponentSchema {
	return []ComponentSchema{
		{"PaywallConfig", Obj(map[string]Schema{
			"headline":        Str(""),
			"headline_accent": Str(""),
			"subtitle":        Str(""),
			"member_count":    Str(""),
			"rating":          Str(""),
			"features":        Arr(Obj(map[string]Schema{"emoji": Str(""), "color": Str(""), "text": Str(""), "bold": Str("")})),
			"reviews":         Arr(Obj(map[string]Schema{"title": Str(""), "username": Str(""), "time_label": Str(""), "description": Str(""), "rating": Int("")})),
			"footer_text":     Str(""),
			"trial_text":      Str(""),
			"cta_text":        Str(""),
			"version":         Int(""),
		})},
	}
}

// AttestRoutes returns OpenAPI routes for the attest package.
func AttestRoutes() []Route {
	return []Route{
		{
			Method: "POST", Path: "/api/v1/attest/challenge",
			Summary: "Generate attestation challenge", Tags: []string{"attest"}, Auth: true,
			Response: &Response{Status: 200, Description: "Challenge", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{"challenge": Str("Base64-encoded challenge")},
			}},
		},
		{
			Method: "POST", Path: "/api/v1/attest/verify",
			Summary: "Verify device attestation", Tags: []string{"attest"}, Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"key_id":      Str("App Attest key ID"),
				"attestation": Str("Base64-encoded attestation"),
				"challenge":   Str("Base64-encoded challenge echo"),
			}, "key_id", "attestation")},
			Response: &Response{Status: 200, Description: "Verification result", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{"verified": Bool(""), "key_id": Str("")},
			}},
		},
	}
}

// AnalyticsRoutes returns OpenAPI routes for the analytics package (admin endpoints).
func AnalyticsRoutes() []Route {
	return []Route{
		{
			Method: "GET", Path: "/admin/api/analytics/dau",
			Summary: "Daily active users time series", Tags: []string{"analytics"}, Auth: false,
			Parameters: []Parameter{
				{Name: "days", In: "query", Description: "Number of days to look back", Schema: Schema{Type: "integer", Default: 30}},
			},
			Response: &Response{Status: 200, Description: "DAU time series", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{
					"dau": Arr(Ref("DAURow")),
				},
			}},
		},
		{
			Method: "GET", Path: "/admin/api/analytics/events",
			Summary: "Event counts grouped by event name", Tags: []string{"analytics"}, Auth: false,
			Parameters: []Parameter{
				{Name: "days", In: "query", Description: "Number of days to look back", Schema: Schema{Type: "integer", Default: 30}},
				{Name: "event", In: "query", Description: "Optional event name filter", Schema: Str("")},
			},
			Response: &Response{Status: 200, Description: "Event counts", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{
					"events": Arr(Ref("EventRow")),
				},
			}},
		},
		{
			Method: "GET", Path: "/admin/api/analytics/mrr",
			Summary: "Subscription and revenue summary", Tags: []string{"analytics"}, Auth: false,
			Response: &Response{Status: 200, Description: "MRR breakdown", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{
					"breakdown":    Arr(Ref("SubStats")),
					"active_total": Int("Total active + trial subscriptions"),
					"new_30d":      Int("New subscriptions in last 30 days"),
					"churned_30d":  Int("Churned subscriptions in last 30 days"),
				},
			}},
		},
		{
			Method: "GET", Path: "/admin/api/analytics/summary",
			Summary: "Overview stats (DAU, MAU, total users, active subs)", Tags: []string{"analytics"}, Auth: false,
			Response: &Response{Status: 200, Description: "Summary stats", Schema: &Schema{
				Type: "object", Properties: map[string]Schema{
					"dau_today":   Int("Daily active users today"),
					"mau":         Int("Monthly active users"),
					"total_users": Int("Total registered users"),
					"active_subs": Int("Active subscriptions"),
				},
			}},
		},
	}
}

// AnalyticsSchemas returns OpenAPI schemas for the analytics package.
func AnalyticsSchemas() []ComponentSchema {
	return []ComponentSchema{
		{"DAURow", Obj(map[string]Schema{
			"date": StrFmt("Date in YYYY-MM-DD format", "date"),
			"dau":  Int("Daily active user count"),
		})},
		{"EventRow", Obj(map[string]Schema{
			"date":         StrFmt("", "date"),
			"event":        Str("Event name"),
			"count":        Int("Total event count"),
			"unique_users": Int("Unique users who triggered this event"),
		})},
		{"SubStats", Obj(map[string]Schema{
			"status": Str("Subscription status"),
			"count":  Int("Number of subscriptions with this status"),
		})},
	}
}

// LifecycleRoutes returns OpenAPI routes for the lifecycle package.
func LifecycleRoutes() []Route {
	return []Route{
		{
			Method: "GET", Path: "/api/v1/user/lifecycle",
			Summary: "Get user lifecycle stage and engagement score", Tags: []string{"lifecycle"}, Auth: true,
			Response: &Response{Status: 200, Description: "Lifecycle data", Schema: &Schema{Ref: "EngagementScore"}},
		},
		{
			Method: "POST", Path: "/api/v1/user/lifecycle/ack",
			Summary: "Acknowledge a lifecycle prompt (shown/accepted/dismissed)", Tags: []string{"lifecycle"}, Auth: true,
			Request: &RequestBody{Required: true, Schema: Obj(map[string]Schema{
				"prompt_type": StrEnum("Prompt type", "review", "paywall", "winback", "milestone"),
				"action":      StrEnum("User action", "shown", "accepted", "dismissed"),
			}, "prompt_type", "action")},
			Response: &Response{Status: 200, Description: "Acknowledged"},
		},
	}
}

// LifecycleSchemas returns OpenAPI schemas for the lifecycle package.
func LifecycleSchemas() []ComponentSchema {
	return []ComponentSchema{
		{"EngagementScore", Obj(map[string]Schema{
			"user_id":           Str(""),
			"stage":             StrEnum("Lifecycle stage", "new", "activated", "engaged", "monetized", "loyal", "at_risk", "dormant", "churned"),
			"score":             IntRange("Engagement score", 0, 100),
			"days_since_active": Int(""),
			"total_sessions":    Int(""),
			"aha_reached":       Bool(""),
			"is_pro":            Bool(""),
			"created_days_ago":  Int(""),
			"prompt":            Ref("LifecyclePrompt"),
		})},
		{"LifecyclePrompt", Obj(map[string]Schema{
			"type":   StrEnum("", "review", "paywall", "winback", "milestone"),
			"title":  Str(""),
			"body":   Str(""),
			"reason": Str(""),
		})},
	}
}

// AllRoutes returns all donkeygo routes combined.
func AllRoutes() []Route {
	var routes []Route
	routes = append(routes, AuthRoutes()...)
	routes = append(routes, NotifyRoutes()...)
	routes = append(routes, EngageRoutes()...)
	routes = append(routes, ChatRoutes()...)
	routes = append(routes, SyncRoutes()...)
	routes = append(routes, PaywallRoutes()...)
	routes = append(routes, AttestRoutes()...)
	routes = append(routes, AnalyticsRoutes()...)
	routes = append(routes, LifecycleRoutes()...)
	return routes
}

// AllSchemas returns all donkeygo schemas combined.
func AllSchemas() []ComponentSchema {
	var schemas []ComponentSchema
	schemas = append(schemas, AuthSchemas()...)
	schemas = append(schemas, NotifySchemas()...)
	schemas = append(schemas, EngageSchemas()...)
	schemas = append(schemas, ChatSchemas()...)
	schemas = append(schemas, SyncSchemas()...)
	schemas = append(schemas, PaywallSchemas()...)
	schemas = append(schemas, AnalyticsSchemas()...)
	schemas = append(schemas, LifecycleSchemas()...)
	return schemas
}
