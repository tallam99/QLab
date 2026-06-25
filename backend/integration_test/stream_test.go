//go:build integration

package integrationtest

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/tallam99/qlab/backend/internal/api"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
)

// sseStream is a minimal Server-Sent Events reader over an open HTTP response: it
// parses "data:" frames (ignoring ":" heartbeat comments) into QueueEvents.
type sseStream struct {
	resp *http.Response
	r    *bufio.Reader
}

func (s *sseStream) close() { _ = s.resp.Body.Close() }

// next reads the next data frame and decodes it, failing the test if none arrives
// within timeout. Heartbeat comments are skipped, so it returns only real events.
func (s *sseStream) next(t *testing.T, timeout time.Duration) *v1.QueueEvent {
	t.Helper()
	type outcome struct {
		evt *v1.QueueEvent
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		var data []byte
		for {
			line, err := s.r.ReadBytes('\n')
			if err != nil {
				ch <- outcome{err: err}
				return
			}
			trimmed := bytes.TrimRight(line, "\r\n")
			switch {
			case len(trimmed) == 0: // frame boundary
				if len(data) > 0 {
					var evt v1.QueueEvent
					ch <- outcome{evt: &evt, err: protojson.Unmarshal(data, &evt)}
					return
				}
				// Blank line with no data buffered (e.g. after a heartbeat comment): keep reading.
			case bytes.HasPrefix(trimmed, []byte(":")): // SSE comment / heartbeat
			case bytes.HasPrefix(trimmed, []byte("data:")):
				data = append(data, bytes.TrimSpace(trimmed[len("data:"):])...)
			}
		}
	}()
	select {
	case o := <-ch:
		require.NoError(t, o.err)
		return o.evt
	case <-time.After(timeout):
		t.Fatal("timed out waiting for an SSE event")
		return nil
	}
}

// openScheduleStream opens the SSE schedule stream for a pool with the given bearer
// token + selected lab. It returns the stream and the HTTP status; on a non-200 the
// stream is nil and the body is already closed (the error cases never stream).
func (h *harness) openScheduleStream(t *testing.T, token, labID, poolID string) (*sseStream, int) {
	t.Helper()
	q := url.Values{"resource_pool_id": {poolID}}.Encode()
	req, err := http.NewRequest(http.MethodGet, h.baseURL+api.StreamSchedulePath+"?"+q, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set(api.HeaderAuthorization, "Bearer "+token)
	}
	if labID != "" {
		req.Header.Set(api.HeaderSelectedLab, labID)
	}
	resp, err := h.http.Do(req)
	require.NoError(t, err)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, resp.StatusCode
	}
	return &sseStream{resp: resp, r: bufio.NewReader(resp.Body)}, resp.StatusCode
}

// TestScheduleStreamLiveUpdate is the heart of Phase 10 PR2: a client subscribed to a
// pool's SSE stream receives the current schedule immediately, then a fresh, typed
// event whenever the pool changes — here, a CreateSlot on another connection, fanned
// out via the transactional NOTIFY → listener → broker → stream path (decision 0010).
func (s *IntegrationSuite) TestScheduleStreamLiveUpdate() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	c := h.client(t, lab.Member1, lab.LabID)
	token := h.tokenFor(t, h.emailOf(t, lab.Member1))

	stream, status := h.openScheduleStream(t, token, lab.LabID, lab.PoolID)
	require.Equal(t, http.StatusOK, status)
	defer stream.close()

	// Initial frame: the current (empty) schedule, kind-less (UNSPECIFIED == "here is
	// the state"), so the client is current the instant it connects.
	snapshot := stream.next(t, 3*time.Second)
	assert.Equal(t, v1.QueueEventType_QUEUE_EVENT_TYPE_UNSPECIFIED, snapshot.GetType())
	assert.Equal(t, lab.PoolID, snapshot.GetResourcePoolId())
	assert.Empty(t, snapshot.GetResult().GetSlots())

	// A mutation on the pool pushes a typed event carrying the recomputed schedule.
	create, err := c.CreateSlot(ctx, createReq(lab.PoolID, at(60), 0, 60, "live"))
	require.NoError(t, err)
	slotID := slotIDByNote(t, create.Msg.GetResult(), "live")

	evt := stream.next(t, 3*time.Second)
	assert.Equal(t, v1.QueueEventType_QUEUE_EVENT_TYPE_SLOT_CREATED, evt.GetType())
	assert.Equal(t, lab.LabID, evt.GetLabId())
	assert.Equal(t, lab.PoolID, evt.GetResourcePoolId())
	require.Len(t, evt.GetResult().GetSlots(), 1)
	assert.Equal(t, slotID, evt.GetResult().GetSlots()[0].GetId())
}

// TestScheduleStreamRequiresAuth guards the stream behind the same bearer-token check
// as every RPC: no token ⇒ 401, never an open stream (decision 0001).
func (s *IntegrationSuite) TestScheduleStreamRequiresAuth() {
	t := s.T()
	lab := h.makeLab(t, 1)

	_, status := h.openScheduleStream(t, "", lab.LabID, lab.PoolID)
	assert.Equal(t, http.StatusUnauthorized, status)
}
