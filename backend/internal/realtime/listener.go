package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tallam99/qlab/backend/internal/logging"
	"github.com/tallam99/qlab/backend/internal/store"
)

// Reconnect backoff for the listener's dedicated connection. A post-connect drop
// (e.g. an idle Neon connection being recycled) reconnects fast; a connect failure
// (database down) backs off up to the cap so a real outage isn't hammered.
const (
	listenBaseDelay = 500 * time.Millisecond
	listenMaxDelay  = 10 * time.Second
)

// Listener holds the process's single LISTEN connection and dispatches schedule
// notifications to the Broker. It owns its own dedicated connection rather than
// borrowing from the request pool because LISTEN needs a session pinned for the
// connection's lifetime — which a transaction-pooled connection (Neon's pooled
// endpoint) does not provide. Point dsn at the DIRECT, unpooled endpoint in the
// cloud (decision 0010); locally the single Postgres has no pooler, so the app DSN
// works as-is.
type Listener struct {
	dsn    string
	broker *Broker
	logger logging.Logger
}

// NewListener builds a Listener that connects with dsn and dispatches to broker.
func NewListener(dsn string, broker *Broker, logger logging.Logger) *Listener {
	return &Listener{dsn: dsn, broker: broker, logger: logger}
}

// Run listens until ctx is cancelled, reconnecting with bounded backoff on any
// connection error. It does not return an error: a listener that can't connect
// degrades the app to no-live-updates (clients still load via GetSchedule and
// reconnect their streams), so it logs and keeps trying rather than failing the
// process. Launch it in a goroutine tied to the server's lifetime.
func (l *Listener) Run(ctx context.Context) {
	backoff := listenBaseDelay
	for ctx.Err() == nil {
		established, err := l.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		if established {
			// The connection worked, so the drop is likely transient — reconnect promptly.
			backoff = listenBaseDelay
		}
		l.logger.Warn("schedule listener disconnected; reconnecting",
			"error", err, "retry_in", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if !established {
			backoff = min(backoff*2, listenMaxDelay)
		}
	}
}

// listen opens one dedicated connection, LISTENs, and dispatches notifications until
// the connection or ctx fails. It reports whether the connection was established (so
// Run can distinguish a transient drop from a database that is down) and the error
// that ended the session.
func (l *Listener) listen(ctx context.Context) (established bool, err error) {
	conn, err := pgx.Connect(ctx, l.dsn)
	if err != nil {
		return false, fmt.Errorf("connect: %w", err)
	}
	// Close on a fresh context: ctx is likely already cancelled on the shutdown path,
	// and we still want the socket released.
	defer func() { _ = conn.Close(context.Background()) }()

	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{store.ScheduleNotifyChannel}.Sanitize()); err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}
	l.logger.Info("schedule listener connected")
	// Close any gap from before this (re)connect: tell current subscribers to refresh.
	l.broker.RefreshAll()

	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, fmt.Errorf("wait for notification: %w", err)
		}
		var sig store.ScheduleNotification
		if err := json.Unmarshal([]byte(notification.Payload), &sig); err != nil {
			// A malformed payload is a bug on the producer side, not a reason to drop the
			// connection; log it and keep listening.
			l.logger.Error("invalid schedule notification payload",
				"error", err, "payload", notification.Payload)
			continue
		}
		l.broker.Publish(sig)
	}
}
