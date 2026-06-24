import { createClient } from "@connectrpc/connect";
import { DevService } from "../protogen/qlab/dev/v1/dev_pb";
import { operatorTransport } from "./operatorTransport";

// operatorClient is the imperative generated client for the operator surface. The
// switcher's operator calls are imperative flows — provision on modal submit, mint
// on user switch, list/get on demand — so they use a direct client rather than the
// declarative Connect-Query hooks the public API (SlotList) uses. It is still the
// generated client (not hand-written fetch); see frontend/ARCHITECTURE.md.
export const operatorClient = createClient(DevService, operatorTransport);
