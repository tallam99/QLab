import { fromJson } from "@bufbuild/protobuf";
import { fetchEventSource } from "@microsoft/fetch-event-source";
import { env } from "../env";
import { type QueueEvent, QueueEventSchema } from "../protogen/qlab/v1/types_pb";
import { getAuthHolder } from "./authHolder";
import { HEADER_AUTHORIZATION, HEADER_SELECTED_LAB } from "./headers";

// subscribeSchedule opens the live-schedule SSE stream for one pool and invokes
// onEvent for each QueueEvent (the first is the current snapshot, then one per
// change). It returns an unsubscribe function; call it to close the stream.
//
// Why fetch-event-source and not the native EventSource: EventSource cannot send an
// Authorization header, but every QLab endpoint requires the Firebase bearer token
// (decision 0001) and we will not put a token in the URL (it leaks into logs and
// history). fetch-event-source is a fetch-based SSE client that sends the same
// headers as the api transport.
//
// Reconnection is handled here rather than by the library's internal retry so each
// attempt reads a FRESH token from the auth holder (the stream can outlive a token's
// ~1h lifetime). Each (re)connection's first frame is the current snapshot, so a gap
// during a disconnect is closed without any extra refetch.
export interface ScheduleStreamHandlers {
  onEvent: (event: QueueEvent) => void;
  // onError is advisory (for logging/telemetry); the stream keeps retrying regardless.
  onError?: (error: unknown) => void;
}

// Reconnect backoff: quick at first, capped, so a brief blip recovers fast but a
// sustained outage isn't hammered.
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30000;

export function subscribeSchedule(
  poolId: string,
  { onEvent, onError }: ScheduleStreamHandlers,
): () => void {
  const baseUrl = env.apiBaseUrl || window.location.origin;
  const url = `${baseUrl}/v1/stream/schedule?resource_pool_id=${encodeURIComponent(poolId)}`;

  let stopped = false;
  let controller: AbortController | null = null;

  const run = async () => {
    let attempt = 0;
    while (!stopped) {
      controller = new AbortController();
      const { getToken, labId } = getAuthHolder();
      const token = await getToken();
      const headers: Record<string, string> = {};
      if (token) {
        headers[HEADER_AUTHORIZATION] = `Bearer ${token}`;
      }
      if (labId) {
        headers[HEADER_SELECTED_LAB] = labId;
      }

      try {
        await fetchEventSource(url, {
          headers,
          signal: controller.signal,
          // Keep streaming when the tab is backgrounded; otherwise the queue would
          // freeze whenever the user looks away.
          openWhenHidden: true,
          onopen: async (response) => {
            if (!response.ok) {
              // A non-2xx (e.g. 401/404) is terminal for this attempt — throw so the
              // library stops its own retry and our loop backs off and re-auths.
              throw new Error(`schedule stream failed: ${response.status}`);
            }
            attempt = 0; // a good connection resets the backoff
          },
          onmessage: (message) => {
            if (!message.data) {
              return; // heartbeat comments arrive as empty data
            }
            onEvent(fromJson(QueueEventSchema, JSON.parse(message.data)));
          },
          // Throwing from onerror stops the library's internal retry, handing control
          // back to our reconnect loop (which mints a fresh token next attempt).
          onerror: (error) => {
            throw error;
          },
        });
        // fetchEventSource resolved: the server closed the stream cleanly. Loop to reconnect.
      } catch (error) {
        if (stopped || controller.signal.aborted) {
          return;
        }
        onError?.(error);
      }
      attempt += 1;
      await delay(Math.min(RECONNECT_BASE_MS * 2 ** (attempt - 1), RECONNECT_MAX_MS));
    }
  };

  void run();

  return () => {
    stopped = true;
    controller?.abort();
  };
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
