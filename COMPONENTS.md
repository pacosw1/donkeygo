# DonkeyGo — Package API Catalog

Shared Go packages for iOS app backends. Interface-based DB, stdlib `http.ServeMux` compatible, zero database driver dependency.

## httputil

JSON request/response helpers.

### Functions

```go
func WriteJSON(w http.ResponseWriter, status int, v any)
func WriteError(w http.ResponseWriter, status int, msg string)
func DecodeJSON(r *http.Request, v any) error
func GetClientIP(r *http.Request) string
```

---

## middleware

HTTP middleware for auth, CORS, rate limiting, logging, and versioning.

### Types

```go
type ContextKey string
const CtxUserID ContextKey = "user_id"

type AuthConfig struct {
    ParseToken func(token string) (userID string, err error)
}

type AdminConfig struct {
    AdminKey     string
    AdminEmail   string
    ParseToken   func(token string) (userID string, err error)
    GetUserEmail func(userID string) (email string, err error)
}

type RateLimiter struct { /* ... */ }
```

### Functions

```go
func RequireAuth(cfg AuthConfig) func(http.HandlerFunc) http.HandlerFunc
func RequireAdmin(cfg AdminConfig) func(http.HandlerFunc) http.HandlerFunc
func CORS(allowedOrigins string) func(http.Handler) http.Handler
func NewRateLimiter(rate int, window time.Duration) *RateLimiter
func RateLimit(rl *RateLimiter) func(http.Handler) http.Handler
func RateLimitFunc(rl *RateLimiter) func(http.HandlerFunc) http.HandlerFunc
func RequestLog(skipPaths ...string) func(http.Handler) http.Handler
func Version(current, minimum string) func(http.Handler) http.Handler
```

---

## auth

Apple Sign In verification and JWT session management.

### DB Interface

```go
type AuthDB interface {
    UpsertUserByAppleSub(id, appleSub, email, name string) (user *User, err error)
    UserByID(id string) (user *User, err error)
}
```

### Types

```go
type User struct {
    ID, AppleSub, Email, Name string
    CreatedAt, LastLoginAt     time.Time
}

type Config struct {
    JWTSecret, AppleBundleID, AppleWebClientID string
    SessionExpiry                               time.Duration
    ProductionEnv                               bool
}

type Service struct { /* ... */ }
```

### Functions

```go
func New(cfg Config, db AuthDB) *Service
func Migrations() []string
func (s *Service) CreateSessionToken(userID string) (string, error)
func (s *Service) ParseSessionToken(tokenStr string) (string, error)
func (s *Service) VerifyAppleIDToken(tokenString string) (sub, email string, err error)
func (s *Service) HandleAppleAuth(w http.ResponseWriter, r *http.Request)
func (s *Service) HandleMe(w http.ResponseWriter, r *http.Request)
func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request)
```

---

## push

Push notification provider interface with APNs and Log/Noop implementations.

### Interface

```go
type Provider interface {
    Send(deviceToken, title, body string) error
    SendWithData(deviceToken, title, body string, data map[string]string) error
    SendSilent(deviceToken string, data map[string]string) error
}
```

### Types

```go
type Config struct {
    KeyPath, KeyID, TeamID, Topic, Environment string
}

type LogProvider struct{}   // logs to stdout
type NoopProvider struct{}  // silently discards
type APNsProvider struct{}  // real APNs
```

### Functions

```go
func NewProvider(cfg Config) (Provider, error)
func NewAPNsProvider(cfg Config) (*APNsProvider, error)
```

---

## migrate

Simple SQL migration runner. Each package exports its own `Migrations()`.

### Types

```go
type Migration struct {
    Name string
    SQL  string
}

type Runner struct { /* ... */ }
```

### Functions

```go
func NewRunner(db *sql.DB) *Runner
func (r *Runner) Add(migrations ...Migration)
func (r *Runner) Run() error
```

### Usage

```go
runner := migrate.NewRunner(db)
for _, sql := range auth.Migrations() {
    runner.Add(migrate.Migration{Name: "auth", SQL: sql})
}
for _, sql := range notify.Migrations() {
    runner.Add(migrate.Migration{Name: "notify", SQL: sql})
}
runner.Run()
```

---

## storage

S3-compatible object storage with AWS Signature V4 signing.

### Types

```go
type Config struct {
    Bucket, Region, Endpoint, AccessKey, SecretKey string
}

type Client struct { /* ... */ }
```

### Functions

```go
func New(cfg Config) *Client
func (c *Client) Put(key, contentType string, data []byte) error
func (c *Client) Get(key string) ([]byte, string, error)
func (c *Client) Configured() bool
```

---

## engage

Event tracking, subscriptions, sessions, paywall eligibility, and feedback.

### DB Interface

```go
type EngageDB interface {
    TrackEvents(userID string, events []EventInput) error
    UpdateSubscription(userID, productID, status string, expiresAt *time.Time) error
    UpdateSubscriptionDetails(userID, originalTransactionID string, priceCents int, currencyCode string) error
    GetSubscription(userID string) (*UserSubscription, error)
    IsProUser(userID string) (bool, error)
    GetEngagementData(userID string) (*EngagementData, error)
    StartSession(userID, sessionID, appVersion, osVersion, country string) error
    EndSession(userID, sessionID string, durationS int) error
    SaveFeedback(userID, feedbackType, message, appVersion string) error
}
```

### Types

```go
type EventInput struct { Event, Metadata, Timestamp string }
type UserSubscription struct { UserID, ProductID, Status string; ExpiresAt, StartedAt *time.Time; UpdatedAt time.Time }
type EngagementData struct { DaysActive, TotalLogs, CurrentStreak, PaywallShownCount, GoalsCompletedTotal int; SubscriptionStatus, LastPaywallDate string }
type Service struct { PaywallTrigger func(*EngagementData) string }
```

### Functions

```go
func New(cfg Config, db EngageDB) *Service
func Migrations() []string
func DefaultPaywallTrigger(data *EngagementData) string
func (s *Service) HandleTrackEvents(w, r)
func (s *Service) HandleUpdateSubscription(w, r)
func (s *Service) HandleSessionReport(w, r)
func (s *Service) HandleGetEligibility(w, r)
func (s *Service) HandleSubmitFeedback(w, r)
```

---

## notify

Device tokens, notification preferences, and background scheduler.

### DB Interface

```go
type NotifyDB interface {
    UpsertDeviceToken(dt *DeviceToken) error
    DisableDeviceToken(userID, token string) error
    EnabledDeviceTokens(userID string) ([]*DeviceToken, error)
    EnsureNotificationPreferences(userID string)
    GetNotificationPreferences(userID string) (*NotificationPreferences, error)
    UpsertNotificationPreferences(p *NotificationPreferences) error
    AllUsersWithNotificationsEnabled() ([]string, error)
    LastNotificationDelivery(userID string) (*NotificationDelivery, error)
    RecordNotificationDelivery(userID, kind, title, body string)
    TrackNotificationOpened(userID, notificationID string) error
}
```

### Types

```go
type DeviceToken struct { ID, UserID, Token, Platform, DeviceModel, OSVersion, AppVersion string; Enabled bool; LastSeenAt time.Time }
type NotificationPreferences struct { UserID, Timezone string; PushEnabled, StopAfterGoal bool; IntervalSeconds, WakeHour, SleepHour int }
type NotificationDelivery struct { ID, UserID, Kind, Title, Body, Status string; SentAt time.Time }
type TickFunc func(userID string, prefs *NotificationPreferences, tokens []*DeviceToken, pushProvider push.Provider)
```

### Functions

```go
func New(db NotifyDB, pushProvider push.Provider) *Service
func Migrations() []string
func NewScheduler(db NotifyDB, pushProvider push.Provider, cfg SchedulerConfig) *Scheduler
func (s *Scheduler) Start()
func (s *Scheduler) Stop()
func (s *Service) HandleRegisterDevice(w, r)
func (s *Service) HandleDisableDevice(w, r)
func (s *Service) HandleGetNotificationPrefs(w, r)
func (s *Service) HandleUpdateNotificationPrefs(w, r)
func (s *Service) HandleNotificationOpened(w, r)
```

---

## sync

Multi-device delta sync with tombstones and batch operations.

### DB Interface

```go
type SyncDB interface {
    ServerTime() (time.Time, error)
    Tombstones(userID string, since time.Time) ([]DeletedEntry, error)
    RecordTombstone(userID, entityType, entityID string) error
}
```

### App Interface

```go
type EntityHandler interface {
    ChangedSince(userID string, since time.Time) (map[string]any, error)
    BatchUpsert(userID string, items []map[string]any) ([]BatchResponseItem, []BatchError)
    Delete(userID, entityType, entityID string) error
}
```

### Functions

```go
func New(db SyncDB, handler EntityHandler) *Service
func Migrations() []string
func (s *Service) HandleSyncChanges(w, r)   // GET /api/v1/sync/changes?since=...
func (s *Service) HandleSyncBatch(w, r)     // POST /api/v1/sync/batch
func (s *Service) HandleSyncDelete(w, r)    // DELETE /api/v1/sync/{entity_type}/{id}
```

---

## chat

WebSocket-based real-time user↔developer chat with push fallback.

### DB Interface

```go
type ChatDB interface {
    GetChatMessages(userID string, limit, offset int) ([]*ChatMessage, error)
    GetChatMessagesSince(userID string, sinceID int) ([]*ChatMessage, error)
    SendChatMessage(userID, sender, message, messageType string) (*ChatMessage, error)
    MarkChatRead(userID, reader string) error
    GetUnreadCount(userID string) (int, error)
    AdminListChatThreads(limit int) ([]*ChatThread, error)
    EnabledDeviceTokens(userID string) ([]string, error)
}
```

### Types

```go
type ChatMessage struct { ID int; UserID, Sender, Message, MessageType string; ReadAt *string; CreatedAt time.Time }
type ChatThread struct { UserID, UserName, UserEmail, LastMessage, LastSender, LastMessageAt string; UnreadCount int }
type Config struct { ParseToken func(string)(string,error); AdminAuth func(*http.Request)bool }
```

### Functions

```go
func New(db ChatDB, pushProvider push.Provider, cfg Config) *Service
func Migrations() []string
func (s *Service) HandleGetChat(w, r)        // GET /api/v1/chat
func (s *Service) HandleSendChat(w, r)       // POST /api/v1/chat
func (s *Service) HandleUnreadCount(w, r)    // GET /api/v1/chat/unread
func (s *Service) HandleAdminListChats(w, r) // GET /admin/api/chat
func (s *Service) HandleAdminGetChat(w, r)   // GET /admin/api/chat/{user_id}
func (s *Service) HandleAdminReplyChat(w, r) // POST /admin/api/chat/{user_id}
func (s *Service) HandleUserWS(w, r)         // GET /api/v1/chat/ws?token=...
func (s *Service) HandleAdminWS(w, r)        // GET /admin/api/chat/ws
```

---

## paywall

Server-driven paywall configuration with multi-locale support.

### Types

```go
type Feature struct { Emoji, Color, Text, Bold string }
type Review struct { Title, Username, TimeLabel, Description string; Rating int }
type Config struct { Headline, HeadlineAccent, Subtitle, MemberCount, Rating, FooterText, TrialText, CTAText string; Features []Feature; Reviews []Review; Version int }
type Store struct { /* ... */ }
```

### Functions

```go
func NewStore(initial map[string]*Config) *Store
func (s *Store) Get(locale string) *Config
func (s *Store) Set(locale string, cfg *Config)
func HandleGetConfig(store *Store) http.HandlerFunc
func HandleUpdateConfig(store *Store) http.HandlerFunc
```

---

## attest

App Attest challenge/verify for device verification.

### DB Interface

```go
type AttestDB interface {
    StoreAttestKey(userID, keyID string) error
    GetAttestKey(userID string) (keyID string, err error)
}
```

### Functions

```go
func New(cfg Config, db AttestDB) *Service
func Migrations() []string
func GenerateHexNonce(bytes int) (string, error)
func (s *Service) HandleChallenge(w, r)                    // POST /api/v1/attest/challenge
func (s *Service) HandleVerify(w, r)                       // POST /api/v1/attest/verify
func (s *Service) RequireAttest(next http.HandlerFunc) http.HandlerFunc
```

---

## logbuf

Ring-buffer log capture for admin panels.

### Types

```go
type LogBuffer struct { /* ... */ }
```

### Functions

```go
func New(capacity int) *LogBuffer
func (b *LogBuffer) Write(p []byte) (int, error)  // implements io.Writer
func (b *LogBuffer) Lines(n int) []string
func SetupLogCapture(buf *LogBuffer)
func HandleAdminLogs(buf *LogBuffer) http.HandlerFunc
```

---

## openapi

Programmatic OpenAPI 3.1 spec generation from donkeygo route definitions. Each package exports Routes() and Schemas() — this package composes them into a versioned spec for Swift SDK generation.

### Types

```go
type Route struct {
    Method, Path, Summary, Description string
    Tags       []string
    Auth       bool
    Request    *RequestBody
    Response   *Response
    Parameters []Parameter
}

type Schema struct {
    Type       string
    Ref        string
    Properties map[string]Schema
    Required   []string
    Items      *Schema
    Format     string
    Enum       []string
    Nullable   bool
    Default    any
    Minimum    *int
    Maximum    *int
}

type RequestBody struct {
    Required    bool
    ContentType string
    Schema      Schema
}

type Response struct {
    Status      int
    Description string
    Schema      *Schema
}

type Parameter struct {
    Name, In, Description string
    Required              bool
    Schema                Schema
}

type SpecConfig struct {
    Title, Description, Version string
    Servers                     []Server
    ExtraSchemas                []ComponentSchema
    ExtraRoutes                 []Route
}

type ComponentSchema struct {
    Name   string
    Schema Schema
}
```

### Functions

```go
// Schema helpers
func Str(desc string) Schema
func StrFmt(desc, format string) Schema
func Int(desc string) Schema
func IntRange(desc string, min, max int) Schema
func Bool(desc string) Schema
func Ref(name string) Schema
func Arr(items Schema) Schema
func Obj(props map[string]Schema, required ...string) Schema
func StrEnum(desc string, values ...string) Schema
func NullStr(desc string) Schema

// Per-package route/schema exports
func AuthRoutes() []Route
func AuthSchemas() []ComponentSchema
func NotifyRoutes() []Route
func NotifySchemas() []ComponentSchema
func EngageRoutes() []Route
func EngageSchemas() []ComponentSchema
func ChatRoutes() []Route
func ChatSchemas() []ComponentSchema
func SyncRoutes() []Route
func SyncSchemas() []ComponentSchema
func PaywallRoutes() []Route
func PaywallSchemas() []ComponentSchema
func AttestRoutes() []Route

// Compose all packages
func AllRoutes() []Route
func AllSchemas() []ComponentSchema

// Generate spec
func Generate(cfg SpecConfig, routes []Route, schemas []ComponentSchema) map[string]any
func GenerateYAML(cfg SpecConfig, routes []Route, schemas []ComponentSchema) string
```

### CLI Usage

```bash
go run github.com/pacosw1/donkeygo/cmd/openapi \
  --title "My App API" \
  --version 1 \
  --prod-url "https://api.myapp.com" \
  > openapi.yaml
```

### Adding App-Specific Routes

```go
appRoutes := openapi.AllRoutes()
appRoutes = append(appRoutes, openapi.Route{
    Method: "GET", Path: "/api/v1/tasks",
    Summary: "List user tasks", Tags: []string{"tasks"}, Auth: true,
    Response: &openapi.Response{Status: 200, Description: "Tasks", Schema: &openapi.Schema{
        Type: "array", Items: &openapi.Schema{Ref: "Task"},
    }},
})

appSchemas := openapi.AllSchemas()
appSchemas = append(appSchemas, openapi.ComponentSchema{
    "Task", openapi.Obj(map[string]openapi.Schema{
        "id":        openapi.Str(""),
        "title":     openapi.Str(""),
        "completed": openapi.Bool(""),
    }),
})

yaml := openapi.GenerateYAML(cfg, appRoutes, appSchemas)
```

---

## App Wiring Example

```go
package main

import (
    "database/sql"
    "net/http"

    "github.com/pacosw1/donkeygo/auth"
    "github.com/pacosw1/donkeygo/chat"
    "github.com/pacosw1/donkeygo/engage"
    "github.com/pacosw1/donkeygo/logbuf"
    "github.com/pacosw1/donkeygo/middleware"
    "github.com/pacosw1/donkeygo/notify"
    "github.com/pacosw1/donkeygo/paywall"
    "github.com/pacosw1/donkeygo/push"
    "github.com/pacosw1/donkeygo/sync"
)

func main() {
    db := openDB()

    // Services
    authSvc := auth.New(auth.Config{JWTSecret: "secret", AppleBundleID: "com.app"}, myDB)
    pushSvc, _ := push.NewProvider(push.Config{KeyPath: "key.p8", KeyID: "ABC", TeamID: "XYZ", Topic: "com.app"})
    engageSvc := engage.New(engage.Config{}, myDB)
    notifySvc := notify.New(myDB, pushSvc)
    chatSvc := chat.New(myDB, pushSvc, chat.Config{ParseToken: authSvc.ParseSessionToken})
    syncSvc := sync.New(myDB, &MyEntityHandler{})
    logBuf := logbuf.New(5000)
    logbuf.SetupLogCapture(logBuf)
    paywallStore := paywall.NewStore(map[string]*paywall.Config{"en": {Headline: "Unlock"}})

    // Middleware
    requireAuth := middleware.RequireAuth(middleware.AuthConfig{ParseToken: authSvc.ParseSessionToken})
    globalRL := middleware.NewRateLimiter(100, time.Minute)
    authRL := middleware.NewRateLimiter(10, time.Minute)

    // Routes
    mux := http.NewServeMux()
    mux.HandleFunc("POST /api/v1/auth/apple", middleware.RateLimitFunc(authRL)(authSvc.HandleAppleAuth))
    mux.HandleFunc("GET /api/v1/auth/me", requireAuth(authSvc.HandleMe))
    mux.HandleFunc("POST /api/v1/events", requireAuth(engageSvc.HandleTrackEvents))
    mux.HandleFunc("PUT /api/v1/subscription", requireAuth(engageSvc.HandleUpdateSubscription))
    mux.HandleFunc("POST /api/v1/notifications/devices", requireAuth(notifySvc.HandleRegisterDevice))
    mux.HandleFunc("GET /api/v1/sync/changes", requireAuth(syncSvc.HandleSyncChanges))
    mux.HandleFunc("GET /api/v1/chat", requireAuth(chatSvc.HandleGetChat))
    mux.HandleFunc("GET /api/v1/chat/ws", chatSvc.HandleUserWS)
    mux.HandleFunc("GET /api/v1/paywall/config", paywall.HandleGetConfig(paywallStore))
    mux.HandleFunc("GET /admin/api/logs", logbuf.HandleAdminLogs(logBuf))

    handler := middleware.RequestLog("/api/health")(middleware.RateLimit(globalRL)(middleware.CORS("*")(mux)))
    http.ListenAndServe(":8080", handler)
}
```
