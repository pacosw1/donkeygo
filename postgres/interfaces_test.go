package postgres

// Compile-time interface compliance checks.
// These ensure *DB and adapters satisfy all donkeygo DB interfaces.

import (
	"github.com/pacosw1/donkeygo/analytics"
	"github.com/pacosw1/donkeygo/attest"
	"github.com/pacosw1/donkeygo/auth"
	"github.com/pacosw1/donkeygo/chat"
	"github.com/pacosw1/donkeygo/engage"
	"github.com/pacosw1/donkeygo/lifecycle"
	"github.com/pacosw1/donkeygo/notify"
	"github.com/pacosw1/donkeygo/receipt"
	gosync "github.com/pacosw1/donkeygo/sync"
)

var _ auth.AuthDB = (*DB)(nil)
var _ attest.AttestDB = (*DB)(nil)
var _ engage.EngageDB = (*DB)(nil)
var _ notify.NotifyDB = (*DB)(nil)
var _ gosync.SyncDB = (*DB)(nil)
var _ gosync.DeviceTokenStore = (*DB)(nil)
var _ receipt.ReceiptDB = (*DB)(nil)
var _ analytics.AnalyticsDB = (*DB)(nil)

// chat.ChatDB and lifecycle.LifecycleDB use adapters because their
// EnabledDeviceTokens returns []string while notify.NotifyDB returns []*DeviceToken.
var _ chat.ChatDB = (*ChatDBAdapter)(nil)
var _ lifecycle.LifecycleDB = (*LifecycleDBAdapter)(nil)
