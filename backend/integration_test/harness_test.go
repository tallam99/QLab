//go:build integration

package integrationtest

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tallam99/qlab/backend/internal/api"
	firebaseclient "github.com/tallam99/qlab/backend/internal/clients/firebase"
	"github.com/tallam99/qlab/backend/internal/devapi"
	"github.com/tallam99/qlab/backend/internal/logging"
	"github.com/tallam99/qlab/backend/internal/protogen/qlab/dev/v1/devv1connect"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/protogen/qlab/v1/qlabv1connect"
	"github.com/tallam99/qlab/backend/internal/server"
	operatorv1 "github.com/tallam99/qlab/backend/internal/services/operator/v1"
	"github.com/tallam99/qlab/backend/internal/store/pgstore"
)

// base anchors every test's clock; tests advance from here. A fixed instant keeps
// timestamps deterministic and readable.
var base = time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)

// at returns the instant m minutes after base.
func at(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }

// testClock is the injected clock; tests set/advance it to drive overrun, grace,
// and pull-forward deterministically. Safe for concurrent reads by the server.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

// harness holds everything a test touches: the admin pool (superuser, bypasses RLS
// — for arranging state and reading ground truth), the running server, the HTTP
// client + base URL for building Connect clients, the clock, the auth helpers (a
// userID→email map and a per-email ID-token cache, minted in-process against the
// real Auth emulator), and the operator-secret used by the DevService.
type harness struct {
	admin          *pgxpool.Pool
	baseURL        string
	http           *http.Client
	clock          *testClock
	cancel         context.CancelFunc
	done           chan struct{}
	minter         *firebaseclient.Minter
	operatorSecret string

	mu     sync.Mutex
	emails map[string]string // userID -> email (for minting that user's token)
	tokens map[string]string // email -> cached ID token
}

// operatorSecret is the gate for the DevService in the suite.
const itestOperatorSecret = "itest-operator-secret"

// startHarness boots the real server (connecting as the app role) on an ephemeral
// port, waits for readiness, and opens the admin pool for arranging state. The
// server verifies tokens against the Auth emulator and mounts the operator
// (qlab.dev.v1) surface over the admin (elevated) pool. The harness mints auth
// tokens in-process via the same Auth emulator.
func startHarness(appURL string) (*harness, error) {
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open admin pool: %w", err)
	}

	// The Admin SDK reads FIREBASE_AUTH_EMULATOR_HOST itself (set by mage), so this
	// client talks to the emulator; the project id is the demo project.
	emulatorHost := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST")
	firebaseAuth, err := firebaseclient.New(ctx, firebaseclient.Options{ProjectID: os.Getenv("FIREBASE_PROJECT_ID")})
	if err != nil {
		admin.Close()
		return nil, fmt.Errorf("build firebase client: %w", err)
	}
	minter := firebaseclient.NewMinter(firebaseAuth, emulatorHost, "")

	// Operator surface over the admin pool (the elevated, RLS-bypassing connection),
	// mounted as the production binary would mount it outside prod.
	operatorStore, err := pgstore.New(ctx, admin)
	if err != nil {
		admin.Close()
		return nil, fmt.Errorf("build operator store: %w", err)
	}
	operatorSvc := operatorv1.New(operatorv1.Options{Store: operatorStore, Minter: minter})
	opPath, opHandler := devapi.New(operatorSvc, itestOperatorSecret).Handler()

	clock := &testClock{now: base}
	srv := server.New(server.Options{
		Logger:        logging.Noop(),
		Addr:          "127.0.0.1:0", // ephemeral; discovered via srv.Addr()
		FirebaseAuth:  firebaseAuth,
		OperatorMount: &server.OperatorMount{Path: opPath, Handler: opHandler},
	})
	srv.InjectDependency(server.WithPostgres(appURL))
	srv.InjectDependency(server.WithAuthentication())
	srv.InjectDependency(server.WithSchedulingService(testGrace, clock.Now))

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Run(runCtx)
	}()

	h := &harness{
		admin:          admin,
		http:           &http.Client{},
		clock:          clock,
		cancel:         cancel,
		done:           done,
		minter:         minter,
		operatorSecret: itestOperatorSecret,
		emails:         make(map[string]string),
		tokens:         make(map[string]string),
	}
	if err := h.waitReady(srv); err != nil {
		cancel()
		<-done
		admin.Close()
		return nil, err
	}
	return h, nil
}

// waitReady blocks until the server has bound its port and /readyq returns 200.
func (h *harness) waitReady(srv *server.Server) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.Addr()
		if addr != "" {
			h.baseURL = "http://" + addr
			resp, err := h.http.Get(h.baseURL + "/readyq")
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("server did not become ready within deadline")
}

func (h *harness) shutdown() {
	h.cancel()
	<-h.done
	h.admin.Close()
}

// client returns a Connect client authenticated as the given user (by minting a
// real emulator ID token for that user's email) acting in the given lab.
func (h *harness) client(t *testing.T, userID, labID string) qlabv1connect.QlabServiceClient {
	t.Helper()
	return h.bearerClient(h.tokenFor(t, h.emailOf(t, userID)), labID)
}

// clientForEmail authenticates as an arbitrary email (which may have no users row),
// for exercising the not-provisioned path.
func (h *harness) clientForEmail(t *testing.T, email, labID string) qlabv1connect.QlabServiceClient {
	t.Helper()
	return h.bearerClient(h.tokenFor(t, email), labID)
}

// anonClient returns a client with no Authorization header (for the unauthenticated
// case).
func (h *harness) anonClient() qlabv1connect.QlabServiceClient {
	return qlabv1connect.NewQlabServiceClient(h.http, h.baseURL)
}

// bearerClient builds a Connect client that stamps the given bearer token and
// selected-lab header on every request — the real auth headers.
func (h *harness) bearerClient(token, labID string) qlabv1connect.QlabServiceClient {
	return qlabv1connect.NewQlabServiceClient(h.http, h.baseURL,
		connect.WithInterceptors(authHeaderInterceptor(token, labID)))
}

// authHeaderInterceptor sets Authorization: Bearer and the selected-lab header on
// every outgoing request.
func authHeaderInterceptor(token, labID string) connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token != "" {
				req.Header().Set(api.HeaderAuthorization, "Bearer "+token)
			}
			if labID != "" {
				req.Header().Set(api.HeaderSelectedLab, labID)
			}
			return next(ctx, req)
		}
	})
}

// emailOf returns the email recorded for a created user.
func (h *harness) emailOf(t *testing.T, userID string) string {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	email, ok := h.emails[userID]
	require.Truef(t, ok, "no email recorded for user %s (was it made via makeUser?)", userID)
	return email
}

// tokenFor returns an emulator ID token for email, minting one in-process against
// the Auth emulator on first use and caching it.
func (h *harness) tokenFor(t *testing.T, email string) string {
	t.Helper()
	h.mu.Lock()
	if tok, ok := h.tokens[email]; ok {
		h.mu.Unlock()
		return tok
	}
	h.mu.Unlock()

	token, err := h.minter.MintToken(context.Background(), email)
	require.NoErrorf(t, err, "mint token for %s", email)
	require.NotEmpty(t, token, "minted an empty token")

	h.mu.Lock()
	h.tokens[email] = token
	h.mu.Unlock()
	return token
}

// operatorClient returns a DevService client carrying the operator secret.
func (h *harness) operatorClient() devv1connect.DevServiceClient {
	return h.operatorClientWithSecret(h.operatorSecret)
}

// operatorClientWithSecret returns a DevService client carrying the given secret
// (used to exercise the gate: a wrong or empty secret is rejected).
func (h *harness) operatorClientWithSecret(secret string) devv1connect.DevServiceClient {
	return devv1connect.NewDevServiceClient(h.http, h.baseURL,
		connect.WithInterceptors(operatorSecretInterceptor(secret)))
}

// operatorSecretInterceptor stamps the operator-secret header on outgoing requests.
func operatorSecretInterceptor(secret string) connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if secret != "" {
				req.Header().Set(devapi.HeaderOperatorSecret, secret)
			}
			return next(ctx, req)
		}
	})
}

// reset returns the database to a clean slate and the clock to base between tests.
// TRUNCATE ... RESTART IDENTITY CASCADE clears every table; it runs as the
// superuser admin, so RLS does not get in the way.
func (h *harness) reset(t *testing.T) {
	t.Helper()
	_, err := h.admin.Exec(context.Background(),
		`TRUNCATE labs, users, labs_users, resource_pools, resources, slots, outbox
		 RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
	h.clock.set(base)
	// Drop per-user identity caches: the next test recreates users with fresh
	// emails, so old mappings/tokens are stale.
	h.mu.Lock()
	h.emails = make(map[string]string)
	h.tokens = make(map[string]string)
	h.mu.Unlock()
}

// ---- state builders (admin connection, bypassing RLS) ----

// labFixture is a ready-made lab: a pool with N resources, a head, and two members.
type labFixture struct {
	LabID   string
	PoolID  string
	Res     []string // resource ids, length == numResources
	Head    string
	Member1 string
	Member2 string
}

// makeLab arranges a lab with numResources interchangeable vent-hood resources and
// three users (head + two members). It commits via the admin pool.
func (h *harness) makeLab(t *testing.T, numResources int) labFixture {
	t.Helper()
	ctx := context.Background()
	f := labFixture{
		LabID:  uuid.NewString(),
		PoolID: uuid.NewString(),
	}
	exec := func(sql string, args ...any) {
		_, err := h.admin.Exec(ctx, sql, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO labs (labs_id, name) VALUES ($1, 'Integration Lab')`, f.LabID)
	f.Head = h.makeUser(t, f.LabID, "HEAD")
	f.Member1 = h.makeUser(t, f.LabID, "MEMBER")
	f.Member2 = h.makeUser(t, f.LabID, "MEMBER")
	exec(`INSERT INTO resource_pools (resource_pools_id, labs_id, kind, name)
	      VALUES ($1, $2, 'VENT_HOOD', 'Vent Hoods')`, f.PoolID, f.LabID)
	for i := 0; i < numResources; i++ {
		rid := uuid.NewString()
		exec(`INSERT INTO resources (resources_id, resource_pools_id, labs_id, kind, name)
		      VALUES ($1, $2, $3, 'VENT_HOOD', $4)`, rid, f.PoolID, f.LabID, fmt.Sprintf("Hood %d", i+1))
		f.Res = append(f.Res, rid)
	}
	return f
}

// makeUser creates a user with a unique email and adds them to labID with role.
func (h *harness) makeUser(t *testing.T, labID, role string) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	email := fmt.Sprintf("%s@example.com", id)
	_, err := h.admin.Exec(ctx, `INSERT INTO users (users_id, email) VALUES ($1, $2)`, id, email)
	require.NoError(t, err)
	_, err = h.admin.Exec(ctx, `INSERT INTO labs_users (labs_id, users_id, role) VALUES ($1, $2, $3)`,
		labID, id, role)
	require.NoError(t, err)
	h.mu.Lock()
	h.emails[id] = email
	h.mu.Unlock()
	return id
}

// slotSpec arranges a pre-existing slot directly (for states the API can't easily
// reach, e.g. an already-ACTIVE overrunning slot). Zero times become NULL columns,
// an empty Resource becomes NULL.
type slotSpec struct {
	ID           string
	Lab          string
	User         string
	Pool         string
	Resource     string
	Priority     int64
	Status       string // slot_status label, e.g. "ACTIVE"
	Desired      time.Time
	Committed    time.Time
	Actual       time.Time
	LookaheadMin int
	DurationMin  int
	Note         string
}

// seedSlot inserts a slot row via the admin pool. It returns the slot id (filling
// one in when the spec leaves it blank).
func (h *harness) seedSlot(t *testing.T, s slotSpec) string {
	t.Helper()
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	_, err := h.admin.Exec(context.Background(),
		`INSERT INTO slots (
			slots_id, labs_id, users_id, resource_pools_id, resources_id,
			slot_priority, desired_start, lookahead, duration,
			committed_start, actual_start, status, note
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::slot_status,$13)`,
		s.ID, s.Lab, s.User, s.Pool, nullStr(s.Resource),
		s.Priority, s.Desired, s.LookaheadMin, s.DurationMin,
		nullTime(s.Committed), nullTime(s.Actual), s.Status, s.Note)
	require.NoError(t, err)
	return s.ID
}

// ---- ground-truth reads (admin connection, bypassing RLS) ----

// slotRow is the persisted state of a slot, read directly for assertions.
type slotRow struct {
	Status    string
	Resource  string // "" when NULL
	Priority  int64
	Desired   time.Time
	Committed time.Time // zero when NULL
	Actual    time.Time // zero when NULL
}

// slot reads a slot's row by id via the admin pool (bypassing RLS).
func (h *harness) slot(t *testing.T, slotID string) slotRow {
	t.Helper()
	var r slotRow
	var resource *string
	var committed, actual *time.Time
	err := h.admin.QueryRow(context.Background(),
		`SELECT status::text, resources_id, slot_priority, desired_start, committed_start, actual_start
		 FROM slots WHERE slots_id = $1`, slotID).
		Scan(&r.Status, &resource, &r.Priority, &r.Desired, &committed, &actual)
	require.NoError(t, err)
	if resource != nil {
		r.Resource = *resource
	}
	if committed != nil {
		r.Committed = *committed
	}
	if actual != nil {
		r.Actual = *actual
	}
	return r
}

// outboxCount returns how many outbox rows of a given event_type exist for a lab.
func (h *harness) outboxCount(t *testing.T, labID, eventType string) int {
	t.Helper()
	var n int
	require.NoError(t, h.admin.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE labs_id = $1 AND event_type = $2`, labID, eventType).Scan(&n))
	return n
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// ---- small request/assert conveniences ----

// createReq builds a CreateSlotRequest for the common case.
func createReq(poolID string, desired time.Time, lookahead, duration int, note string) *connect.Request[v1.CreateSlotRequest] {
	return connect.NewRequest(&v1.CreateSlotRequest{
		ResourcePoolId:   poolID,
		DesiredStart:     tspb(desired),
		LookaheadMinutes: int32(lookahead),
		DurationMinutes:  int32(duration),
		Note:             note,
	})
}

// tspb converts an instant to a protobuf timestamp (nil for the zero value).
func tspb(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// slotIDByNote finds the slot carrying note in a result's slot list — the way a
// test recovers a slot's id after creating it (notes are unique per create).
func slotIDByNote(t *testing.T, r *v1.RescheduleResult, note string) string {
	t.Helper()
	for _, s := range r.GetSlots() {
		if s.GetNote() == note {
			return s.GetId()
		}
	}
	t.Fatalf("no slot with note %q in result", note)
	return ""
}

// positionFor returns the engine verdict for slotID in a result.
func positionFor(t *testing.T, r *v1.RescheduleResult, slotID string) *v1.SlotPosition {
	t.Helper()
	for _, p := range r.GetPositions() {
		if p.GetSlotId() == slotID {
			return p
		}
	}
	t.Fatalf("no position for slot %s in result", slotID)
	return nil
}
