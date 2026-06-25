import { create } from "@bufbuild/protobuf";
import { createRouterTransport } from "@connectrpc/connect";
import { TransportProvider, createConnectQueryKey } from "@connectrpc/connect-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { GetScheduleResponseSchema, QlabService } from "../protogen/qlab/v1/service_pb";
import { QueueEventSchema, RescheduleResultSchema } from "../protogen/qlab/v1/types_pb";
import { usePoolScheduleStream } from "./usePoolScheduleStream";

// Mock the SSE client so the test drives events synchronously, with no network. The
// hoisted holder captures the onEvent handler the hook registers and the unsubscribe.
const sse = vi.hoisted(() => ({
  onEvent: null as ((event: unknown) => void) | null,
  unsubscribe: vi.fn(),
}));
vi.mock("./scheduleStream", () => ({
  subscribeSchedule: (_poolId: string, handlers: { onEvent: (event: unknown) => void }) => {
    sse.onEvent = handlers.onEvent;
    return sse.unsubscribe;
  },
}));

// An empty router transport: the hook never makes RPCs, but it needs a real Transport
// from the provider so its query key matches the one the test builds to read the cache.
const transport = createRouterTransport(() => {});

function wrapper(queryClient: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <TransportProvider transport={transport}>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </TransportProvider>
  );
}

describe("usePoolScheduleStream", () => {
  // A streamed event must land in the exact GetSchedule query cache PoolPanel reads, so
  // a change pushed by the server re-renders the view with no refetch.
  it("writes each event's result into the GetSchedule query cache", () => {
    const queryClient = new QueryClient();
    renderHook(() => usePoolScheduleStream("pool-1"), { wrapper: wrapper(queryClient) });

    expect(sse.onEvent).toBeTypeOf("function");

    const result = create(RescheduleResultSchema, { resourcePoolId: "pool-1" });
    act(() => {
      sse.onEvent?.(create(QueueEventSchema, { result }));
    });

    const cached = queryClient.getQueryData(
      createConnectQueryKey({
        schema: QlabService.method.getSchedule,
        input: { resourcePoolId: "pool-1" },
        transport,
        cardinality: "finite",
      }),
    );
    expect(cached).toEqual(create(GetScheduleResponseSchema, { result }));
  });

  // A null pool (no pool selected) must not subscribe — the hook is a no-op until one is.
  it("does not subscribe when poolId is null", () => {
    sse.onEvent = null;
    const queryClient = new QueryClient();
    renderHook(() => usePoolScheduleStream(null), { wrapper: wrapper(queryClient) });
    expect(sse.onEvent).toBeNull();
  });

  // Unmounting must tear the subscription down so a backgrounded pool doesn't leak streams.
  it("unsubscribes on unmount", () => {
    const queryClient = new QueryClient();
    const { unmount } = renderHook(() => usePoolScheduleStream("pool-1"), {
      wrapper: wrapper(queryClient),
    });
    unmount();
    expect(sse.unsubscribe).toHaveBeenCalled();
  });
});
