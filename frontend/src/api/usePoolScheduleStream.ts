import { create } from "@bufbuild/protobuf";
import { createConnectQueryKey, useTransport } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { GetScheduleResponseSchema, QlabService } from "../protogen/qlab/v1/service_pb";
import { subscribeSchedule } from "./scheduleStream";

// usePoolScheduleStream keeps the GetSchedule query cache live. While poolId is set it
// subscribes to that pool's SSE stream and writes each event's result into the SAME
// query key PoolPanel reads from, so a change made by anyone (any client, any browser)
// re-renders the view with no refetch. It reopens when poolId changes and tears the
// subscription down on unmount. See api/scheduleStream.ts and decision 0010.
export function usePoolScheduleStream(poolId: string | null): void {
  const transport = useTransport();
  const queryClient = useQueryClient();

  useEffect(() => {
    if (!poolId) {
      return;
    }
    // The exact key the GetSchedule useQuery uses, so setQueryData updates that cache
    // entry rather than creating an orphan.
    const queryKey = createConnectQueryKey({
      schema: QlabService.method.getSchedule,
      input: { resourcePoolId: poolId },
      transport,
      cardinality: "finite",
    });

    return subscribeSchedule(poolId, {
      onEvent: (event) => {
        if (event.result) {
          queryClient.setQueryData(
            queryKey,
            create(GetScheduleResponseSchema, { result: event.result }),
          );
        }
      },
    });
  }, [poolId, transport, queryClient]);
}
