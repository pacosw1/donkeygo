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
type EventHook func(userID string, events []EventInput)
type Config struct { PaywallTrigger func(*EngagementData) string }
type Service struct { PaywallTrigger func(*EngagementData) string }
```

### Functions

```go
func New(cfg Config, db EngageDB) *Service
func Migrations() []string
func DefaultPaywallTrigger(data *EngagementData) string
func (s *Service) RegisterEventHook(hook EventHook)  // callback after events tracked
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

Multi-device delta sync with version-based conflict detection, idempotent batch writes, and push-triggered propagation.

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
    ChangedSince(userID string, since time.Time, excludeDeviceID string) (map[string]any, error)
    BatchUpsert(userID, deviceID string, items []BatchItem) ([]BatchResponseItem, []BatchError)
    Delete(userID, entityType, entityID string) error
}
```

### Types

```go
const HeaderDeviceID       = "X-Device-ID"
const HeaderIdempotencyKey = "X-Idempotency-Key"

type BatchItem struct { ClientID, EntityType, EntityID string; Version int; Fields map[string]any }
type BatchResponseItem struct { ClientID, ServerID string; Version int }
type BatchError struct { ClientID, Error string; IsConflict bool; ServerVersion int }
type BatchResponse struct { Items []BatchResponseItem; Errors []BatchError; SyncedAt time.Time }

type DeviceTokenStore interface {
    EnabledTokensForUser(userID string) ([]DeviceInfo, error)
}
type DeviceInfo struct { DeviceID, Token string }

type Config struct {
    Push           push.Provider    // optional silent sync notifications
    DeviceTokens   DeviceTokenStore // required when Push is set
    IdempotencyTTL time.Duration    // default 24h
}
```

### Functions

```go
func New(db SyncDB, handler EntityHandler, cfg ...Config) *Service
func Migrations() []string
func (s *Service) Close()
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

## analytics

Admin analytics dashboard handlers — DAU, MAU, MRR, event counts, summary.

### DB Interface

```go
type AnalyticsDB interface {
    DAUTimeSeries(since time.Time) ([]DAURow, error)
    EventCounts(since time.Time, event string) ([]EventRow, error)
    SubscriptionBreakdown() ([]SubStats, error)
    NewSubscriptions30d() (int, error)
    ChurnedSubscriptions30d() (int, error)
    DAUToday() (int, error)
    MAU() (int, error)
    TotalUsers() (int, error)
    ActiveSubscriptions() (int, error)
}
```

### Types

```go
type DAURow struct { Date string; DAU int }
type EventRow struct { Date, Event string; Count, UniqueUsers int }
type SubStats struct { Status string; Count int }
type Config struct { AppOpenEvent string }  // default "app_open"
```

### Functions

```go
func New(cfg Config, db AnalyticsDB) *Service
func (s *Service) HandleDAU(w, r)       // GET /admin/api/analytics/dau?days=30
func (s *Service) HandleEvents(w, r)    // GET /admin/api/analytics/events?days=30&event=tap
func (s *Service) HandleMRR(w, r)       // GET /admin/api/analytics/mrr
func (s *Service) HandleSummary(w, r)   // GET /admin/api/analytics/summary
```

---

## lifecycle

User lifecycle engine — stages, engagement scoring, aha moments, prompt decisions.

### DB Interface

```go
type LifecycleDB interface {
    UserCreatedAndLastActive(userID string) (createdAt, lastActiveAt time.Time, err error)
    CountSessions(userID string) (int, error)
    CountRecentSessions(userID string, since time.Time) (int, error)
    CountDistinctEventDays(userID, eventName string, since time.Time) (int, error)
    IsProUser(userID string) (bool, error)
    LastPrompt(userID string) (promptType string, promptAt time.Time, err error)
    CountPrompts(userID, promptType string, since time.Time) (int, error)
    RecordPrompt(userID, event, metadata string) error
    EnabledDeviceTokens(userID string) ([]string, error)
}
```

### Types

```go
type LifecycleStage string  // "new", "activated", "engaged", "monetized", "loyal", "at_risk", "dormant", "churned"

type AhaMomentRule struct { Name, Description, EventName string; Threshold, WindowDays int }
type EngagementScore struct {
    UserID string; Stage LifecycleStage; Score int; DaysSinceActive, TotalSessions int
    AhaReached, IsPro bool; CreatedDaysAgo int; ShouldPrompt *LifecyclePrompt
}
type LifecyclePrompt struct { Type, Title, Body, Reason string }

type StageRule struct {
    Name string; Stage Stage
    Matches func(score, daysSinceActive, createdDaysAgo int, ahaReached, isPro bool) bool
}

type Config struct {
    AhaMomentRules     []AhaMomentRule
    CustomStages       []StageRule     // evaluated before built-in rules, first match wins
    PromptBuilder      func(userID string, es *EngagementScore) (*Prompt, error) // override prompt logic
    PromptCooldownDays int             // default 3
}
```

### Functions

```go
func New(cfg Config, db LifecycleDB, push push.Provider) *Service
func (s *Service) EvaluateUser(userID string) (*EngagementScore, error)
func (s *Service) DeterminePrompt(score *EngagementScore) *LifecyclePrompt
func (s *Service) EvaluateNotifications(userIDs []string)
func (s *Service) HandleGetLifecycle(w, r)       // GET /api/v1/user/lifecycle
func (s *Service) HandleAckLifecyclePrompt(w, r) // POST /api/v1/user/lifecycle/ack
```

### Lifecycle Stages

```
NEW → ACTIVATED → ENGAGED → MONETIZED → LOYAL
                                ↕
                          AT_RISK → DORMANT → CHURNED
```

---

## scheduler

Pluggable background task scheduler. Run arbitrary tasks at configurable intervals.

### Interface

```go
type Task interface {
    Name() string
    Run(ctx context.Context) error
}
```

### Types

```go
type FuncTask struct { /* ... */ }

type TaskConfig struct {
    Task     Task
    Every    int  // run every N ticks (1 = every tick, 96 = daily at 15min intervals)
    RunFirst bool // run immediately on first tick
}

type Config struct {
    Interval time.Duration // tick interval (default 15 min)
    Tasks    []TaskConfig
    Logger   *log.Logger   // optional custom logger
}

type Scheduler struct { /* ... */ }
```

### Functions

```go
func New(cfg Config) *Scheduler
func NewFuncTask(name string, fn func(ctx context.Context) error) *FuncTask
func (s *Scheduler) Start()
func (s *Scheduler) Stop()
func (s *Scheduler) AddTask(tc TaskConfig)  // thread-safe, call after Start
func (s *Scheduler) TickCount() int
```

### Usage

```go
s := scheduler.New(scheduler.Config{
    Interval: 15 * time.Minute,
    Tasks: []scheduler.TaskConfig{
        {
            Task:     scheduler.NewFuncTask("cleanup", func(ctx context.Context) error { return cleanExpired(ctx) }),
            Every:    1,   // every tick
        },
        {
            Task:     scheduler.NewFuncTask("daily-report", func(ctx context.Context) error { return sendReport(ctx) }),
            Every:    96,  // every 96 ticks = daily at 15min intervals
            RunFirst: true,
        },
    },
})
s.Start()
defer s.Stop()
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

## receipt

App Store Server API v2 receipt verification with JWS x5c chain validation against Apple Root CA - G3.

### DB Interface

```go
type ReceiptDB interface {
    UpsertSubscription(userID, productID, originalTransactionID, status string, expiresAt *time.Time, priceCents int, currencyCode string) error
    UserIDByTransactionID(originalTransactionID string) (string, error)
    StoreTransaction(t *VerifiedTransaction) error
}
```

### Types

```go
type TransactionInfo struct {
    TransactionID, OriginalTransactionID, BundleID, ProductID string
    PurchaseDate, ExpiresDate, OriginalPurchaseDate           int64
    Type, InAppOwnershipType, Environment, Currency, Storefront string
    Price, OfferType int; RevocationDate int64; AppAccountToken string
}

type NotificationPayload struct { NotificationType, Subtype string; Data NotificationData; Version string; SignedDate int64 }
type NotificationData struct { SignedTransactionInfo, SignedRenewalInfo, Environment, BundleID, BundleVersion string; AppAppleID int64 }
type VerifiedTransaction struct {
    TransactionID, OriginalTransactionID, UserID, ProductID, Status string
    PurchaseDate time.Time; ExpiresDate *time.Time
    Environment, CurrencyCode, NotificationType string; PriceCents int
}
type VerifyResponse struct { Verified bool; Status, ProductID, TransactionID string; ExpiresAt *time.Time }

type Config struct {
    BundleID    string // reject transactions with a different bundle ID
    Environment string // "Production" or "Sandbox"; empty = accept both
}
```

### Functions

```go
func New(db ReceiptDB, cfg Config) *Service
func Migrations() []string
func (s *Service) HandleVerifyReceipt(w, r)  // POST /api/v1/receipt/verify  (auth required)
func (s *Service) HandleWebhook(w, r)        // POST /api/v1/receipt/webhook (no auth — Apple calls directly)
```

---

## postgres

PostgreSQL implementation of all donkeygo DB interfaces. Single `DB` struct, uses `database/sql`, no driver dependency.

### Types

```go
type DB struct { /* wraps *sql.DB */ }

// Adapters resolve EnabledDeviceTokens return type conflicts between
// notify.NotifyDB ([]*DeviceToken) and chat.ChatDB / lifecycle.LifecycleDB ([]string).
type ChatDBAdapter struct { *DB }
type LifecycleDBAdapter struct { *DB }
```

### Interfaces Implemented

| Type                  | Interface                |
|-----------------------|--------------------------|
| `*DB`                 | `auth.AuthDB`            |
| `*DB`                 | `attest.AttestDB`        |
| `*DB`                 | `engage.EngageDB`        |
| `*DB`                 | `notify.NotifyDB`        |
| `*DB`                 | `sync.SyncDB`            |
| `*DB`                 | `sync.DeviceTokenStore`  |
| `*DB`                 | `receipt.ReceiptDB`      |
| `*DB`                 | `account.AccountDB`      |
| `*DB`                 | `admin.AdminDB`          |
| `*DB`                 | `flags.FlagsDB`          |
| `*DB`                 | `analytics.AnalyticsDB`  |
| `*ChatDBAdapter`      | `chat.ChatDB`            |
| `*LifecycleDBAdapter` | `lifecycle.LifecycleDB`  |

### Functions

```go
func New(db *sql.DB) *DB
func (d *DB) SQL() *sql.DB  // access underlying *sql.DB
```

### Usage

```go
import _ "github.com/lib/pq"

db, _ := sql.Open("postgres", connStr)
store := postgres.New(db)

authSvc    := auth.New(authCfg, store)
engageSvc  := engage.New(engage.Config{}, store)
notifySvc  := notify.New(store, pushSvc)
chatSvc    := chat.New(&postgres.ChatDBAdapter{store}, pushSvc, chatCfg)
lifecycleSvc := lifecycle.New(lcCfg, &postgres.LifecycleDBAdapter{store}, pushSvc)
```

---

## admin

Pre-built, extensible admin panel using html/template + HTMX. Dark theme, single binary, zero JS build step. Ships built-in tabs for all donkeygo packages with extension points for app-specific data.

### DB Interface

```go
type AdminDB interface {
    AdminListUsers(search string, limit, offset int) ([]AdminUser, int, error)
    AdminGetUser(userID string) (*AdminUserDetail, error)
    AdminListEvents(eventType, userID string, since time.Time, limit int) ([]AdminEvent, error)
    AdminListNotifications(limit int) ([]AdminNotification, error)
    AdminSubscriptionBreakdown() ([]SubBreakdownRow, error)
    AdminListFeedback(limit int) ([]AdminFeedback, error)
}
```

### Types

```go
type Config struct {
    JWTSecret        string
    SessionExpiry    time.Duration    // default 7 days
    AllowedEmails    []string         // email whitelist
    AdminKey         string           // optional API key
    Production       bool             // secure cookies
    VerifyToken      func(idToken string) (sub, email string, err error)
    AppleWebClientID string
    AppName          string           // default "Admin"
}

type Tab struct { ID, Label, Icon string; Handler http.Handler; Order int }
type Column struct { Header string; Value func(row any) string }
type Section struct { Title string; Handler http.Handler }
type Card struct { Label string; Value func() string; Color string }

type ChatProvider interface {
    HandleAdminListChats(w, r)
    HandleAdminGetChat(w, r)
    HandleAdminReplyChat(w, r)
    HandleAdminWS(w, r)
}

type LogProvider interface { Lines(n int) []string }
```

### Built-in Tab Constructors

```go
func OverviewTab(db analytics.AnalyticsDB, cfg ...OverviewConfig) Tab
func UsersTab(db AdminDB, cfg ...UsersConfig) Tab
func EventsTab(db AdminDB, cfg ...EventsConfig) Tab
func SubscriptionsTab(analyticsDB analytics.AnalyticsDB, adminDB AdminDB, cfg ...SubscriptionsConfig) Tab
func NotificationsTab(db AdminDB, cfg ...NotificationsConfig) Tab
func FeedbackTab(db AdminDB, cfg ...FeedbackConfig) Tab
func ChatTab(chatSvc ChatProvider) Tab
func LogsTab(buf LogProvider) Tab
```

### Extension Configs

```go
type OverviewConfig struct { ExtraCards []Card }
type UsersConfig struct { ExtraColumns []Column; ExtraSections []Section }
type EventsConfig struct { EventTypes []string; ExtraColumns []Column }
type SubscriptionsConfig struct { ExtraCards []Card }
type NotificationsConfig struct { ExtraColumns []Column }
type FeedbackConfig struct { ExtraColumns []Column }
```

### Functions

```go
func New(cfg Config) *Panel
func (p *Panel) Register(tab Tab)
func (p *Panel) ServeHTTP(w, r)  // implements http.Handler — mount at /admin/
```

### Usage

```go
panel := admin.New(admin.Config{
    JWTSecret:        "secret",
    AllowedEmails:    []string{"admin@example.com"},
    VerifyToken:      authSvc.VerifyAppleIDToken,
    AppleWebClientID: "com.app.web",
    AppName:          "My App Admin",
    Production:       true,
})

// Built-in tabs (each optional)
panel.Register(admin.OverviewTab(analyticsSvc.DB()))
panel.Register(admin.UsersTab(adminDB, admin.UsersConfig{
    ExtraColumns: []admin.Column{
        {Header: "Streak", Value: func(row any) string {
            u := row.(admin.AdminUser)
            streak, _ := myDB.GetStreak(u.ID)
            return strconv.Itoa(streak)
        }},
    },
}))
panel.Register(admin.EventsTab(adminDB, admin.EventsConfig{
    EventTypes: []string{"water_logged", "habit_completed"},
}))
panel.Register(admin.SubscriptionsTab(analyticsSvc.DB(), adminDB))
panel.Register(admin.NotificationsTab(adminDB))
panel.Register(admin.FeedbackTab(adminDB))
panel.Register(admin.ChatTab(chatSvc))
panel.Register(admin.LogsTab(logBuf))

// Custom app-specific tab
panel.Register(admin.Tab{
    ID: "inventory", Label: "Inventory", Icon: "package", Order: 90,
    Handler: http.HandlerFunc(myInventoryHandler),
})

mux.Handle("/admin/", panel)
```

---

## health

Liveness and readiness check endpoints for load balancers and orchestrators.

### Functions

```go
func New(cfg Config) *Service
func (s *Service) HandleHealth(w, r)  // GET /health — always 200, liveness probe
func (s *Service) HandleReady(w, r)   // GET /ready — runs all checks, 200 or 503
```

### Types

```go
type Check struct { Name string; Fn func() error }
type Config struct { Checks []Check }
```

### Usage

```go
healthSvc := health.New(health.Config{
    Checks: []health.Check{
        {Name: "db", Fn: func() error { return db.Ping() }},
    },
})
mux.HandleFunc("GET /health", healthSvc.HandleHealth)
mux.HandleFunc("GET /ready", healthSvc.HandleReady)
```

---

## email

Transactional email provider interface with SMTP, Log, and Noop implementations. Follows push.Provider pattern.

### Interface

```go
type Provider interface {
    Send(to, subject, textBody, htmlBody string) error
}
```

### Types

```go
type SMTPConfig struct { Host string; Port int; Username, Password, From, FromName string }
type Template struct { Subject, HTML, Text string }  // Go template strings
type Renderer struct { /* ... */ }
type LogProvider struct{}   // logs to stdout
type NoopProvider struct{}  // silently discards
type SMTPProvider struct{}  // real SMTP
```

### Functions

```go
func NewProvider(cfg SMTPConfig) (Provider, error)  // SMTP if Host set, Log otherwise
func NewSMTPProvider(cfg SMTPConfig) (*SMTPProvider, error)
func NewRenderer() *Renderer
func (r *Renderer) Register(name string, tmpl Template)
func (r *Renderer) Render(name string, data map[string]any) (subject, html, text string, err error)
```

### Usage

```go
provider, _ := email.NewProvider(email.SMTPConfig{Host: "smtp.gmail.com", Port: 587, From: "app@example.com"})
provider.Send("user@example.com", "Welcome!", "Thanks for signing up.", "<h1>Welcome!</h1>")

// With templates:
renderer := email.NewRenderer()
renderer.Register("welcome", email.Template{
    Subject: "Welcome to {{.AppName}}",
    HTML:    "<h1>Hi {{.Name}}</h1>",
    Text:    "Hi {{.Name}}, welcome!",
})
subj, html, text, _ := renderer.Render("welcome", map[string]any{"AppName": "MyApp", "Name": "Paco"})
provider.Send("user@example.com", subj, text, html)
```

---

## flags

Feature flags with user targeting and percentage rollouts. Stored in DB, no external service dependency.

### DB Interface

```go
type FlagsDB interface {
    UpsertFlag(f *Flag) error
    GetFlag(key string) (*Flag, error)
    ListFlags() ([]*Flag, error)
    DeleteFlag(key string) error
    GetUserOverride(key, userID string) (*bool, error)
    SetUserOverride(key, userID string, enabled bool) error
    DeleteUserOverride(key, userID string) error
}
```

### Types

```go
type Flag struct { Key string; Enabled bool; RolloutPct int; Description string; CreatedAt, UpdatedAt time.Time }
```

### Functions

```go
func New(cfg Config, db FlagsDB) *Service
func Migrations() []string
func (s *Service) IsEnabled(key, userID string) (bool, error)  // override > rollout % > default
func (s *Service) HandleCheck(w, r)        // GET /api/v1/flags/{key}
func (s *Service) HandleBatchCheck(w, r)   // POST /api/v1/flags/check
func (s *Service) HandleAdminList(w, r)    // GET /admin/api/flags
func (s *Service) HandleAdminCreate(w, r)  // POST /admin/api/flags
func (s *Service) HandleAdminUpdate(w, r)  // PUT /admin/api/flags/{key}
func (s *Service) HandleAdminDelete(w, r)  // DELETE /admin/api/flags/{key}
```

### Rollout Logic

1. Check user-specific override → if found, return it
2. Check flag enabled → if disabled, return false
3. Hash `key:userID` deterministically → compare against `rollout_pct`

---

## account

GDPR-compliant account deletion, anonymization, and data export. Cascades across all donkeygo tables.

### DB Interface

```go
type AccountDB interface {
    GetUserEmail(userID string) (string, error)
    DeleteUserData(userID string) error   // all donkeygo tables except users
    DeleteUser(userID string) error       // users table
    AnonymizeUser(userID string) error    // replace PII, keep analytics
    ExportUserData(userID string) (*UserDataExport, error)
}
```

### Types

```go
type Config struct { OnDelete func(userID, email string) }
type AppCleanup interface { DeleteAppData(userID string) error }
type AppExporter interface { ExportAppData(userID string) (any, error) }
type UserDataExport struct { User, Subscription, Events, Sessions, Feedback, ChatMessages, DeviceTokens, Preferences, Transactions, AppData any }
```

### Functions

```go
func New(cfg Config, db AccountDB, opts ...any) *Service  // opts: AppCleanup, AppExporter
func (s *Service) HandleDeleteAccount(w, r)     // DELETE /api/v1/account
func (s *Service) HandleAnonymizeAccount(w, r)  // POST /api/v1/account/anonymize
func (s *Service) HandleExportData(w, r)        // GET /api/v1/account/export
```

### Deletion Order

1. `AppCleanup.DeleteAppData()` — app tables first (may FK to users)
2. `AccountDB.DeleteUserData()` — all donkeygo tables
3. `AccountDB.DeleteUser()` — users table last
4. `Config.OnDelete()` callback (e.g. send confirmation email)

---

## App Wiring Example

```go
package main

import (
    "database/sql"
    "net/http"

    _ "github.com/lib/pq"

    "github.com/pacosw1/donkeygo/auth"
    "github.com/pacosw1/donkeygo/chat"
    "github.com/pacosw1/donkeygo/engage"
    "github.com/pacosw1/donkeygo/logbuf"
    "github.com/pacosw1/donkeygo/middleware"
    "github.com/pacosw1/donkeygo/notify"
    "github.com/pacosw1/donkeygo/paywall"
    "github.com/pacosw1/donkeygo/postgres"
    "github.com/pacosw1/donkeygo/push"
    "github.com/pacosw1/donkeygo/receipt"
    "github.com/pacosw1/donkeygo/sync"
)

func main() {
    db := openDB()
    store := postgres.New(db)

    // Services
    authSvc := auth.New(auth.Config{JWTSecret: "secret", AppleBundleID: "com.app"}, store)
    pushSvc, _ := push.NewProvider(push.Config{KeyPath: "key.p8", KeyID: "ABC", TeamID: "XYZ", Topic: "com.app"})
    engageSvc := engage.New(engage.Config{}, store)
    notifySvc := notify.New(store, pushSvc)
    chatSvc := chat.New(&postgres.ChatDBAdapter{store}, pushSvc, chat.Config{ParseToken: authSvc.ParseSessionToken})
    receiptSvc := receipt.New(store, receipt.Config{BundleID: "com.app", Environment: "Production"})
    syncSvc := sync.New(store, &MyEntityHandler{}, sync.Config{Push: pushSvc, DeviceTokens: store})
    defer syncSvc.Close()
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
    mux.HandleFunc("POST /api/v1/receipt/verify", requireAuth(receiptSvc.HandleVerifyReceipt))
    mux.HandleFunc("POST /api/v1/receipt/webhook", receiptSvc.HandleWebhook)
    mux.HandleFunc("GET /api/v1/chat", requireAuth(chatSvc.HandleGetChat))
    mux.HandleFunc("GET /api/v1/chat/ws", chatSvc.HandleUserWS)
    mux.HandleFunc("GET /api/v1/paywall/config", paywall.HandleGetConfig(paywallStore))
    mux.HandleFunc("GET /admin/api/logs", logbuf.HandleAdminLogs(logBuf))

    handler := middleware.RequestLog("/api/health")(middleware.RateLimit(globalRL)(middleware.CORS("*")(mux)))
    http.ListenAndServe(":8080", handler)
}
```
