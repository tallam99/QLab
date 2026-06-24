import { createRouterTransport } from "@connectrpc/connect";
import { TransportProvider } from "@connectrpc/connect-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { QlabService } from "../protogen/qlab/v1/service_pb";
import { SlotStatus } from "../protogen/qlab/v1/types_pb";
import { SlotList } from "./SlotList";

// Drive SlotList off a fixed workspace selection so the test exercises the data path
// (the generated client + Connect-Query + rendering) without the operator context.
vi.mock("../workspace/WorkspaceProvider", () => ({
  useWorkspace: () => ({ poolId: "pool-1", canQuery: true }),
}));

// renderWithTransport wraps SlotList in the same providers main.tsx uses, but with an
// in-memory transport that answers the RPCs locally — no network, deterministic.
function renderWithTransport(slots: unknown[]) {
  const transport = createRouterTransport(({ service }) => {
    service(QlabService, {
      // biome-ignore lint/suspicious/noExplicitAny: partial message init is fine for a stub.
      listSlots: () => ({ slots: slots as any }),
      createSlot: () => ({}),
      cancelSlot: () => ({}),
    });
  });
  const queryClient = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <TransportProvider transport={transport}>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </TransportProvider>
  );
  return render(<SlotList />, { wrapper });
}

// SlotList renders the acting-as user's pool queue; verify it lists the slots and the
// empty state, proving the generated client wiring works.
describe("SlotList", () => {
  it("renders the slots returned by ListSlots", async () => {
    renderWithTransport([
      { id: "slot-1", slotPriority: 1, status: SlotStatus.SCHEDULED, durationMinutes: 30 },
      { id: "slot-2", slotPriority: 2, status: SlotStatus.ACTIVE, durationMinutes: 45 },
    ]);

    expect(await screen.findByText("SCHEDULED")).toBeInTheDocument();
    expect(screen.getByText("ACTIVE")).toBeInTheDocument();
    expect(screen.getByText("30m")).toBeInTheDocument();
  });

  it("shows an empty state when the pool has no slots", async () => {
    renderWithTransport([]);
    expect(await screen.findByText(/no slots in this pool yet/i)).toBeInTheDocument();
  });
});
