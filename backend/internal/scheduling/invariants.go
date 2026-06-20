package scheduling

// CheckInvariants asserts the post-conditions every Reschedule output must hold
// against the Input that produced it (§4):
//
//   - per-bench no-overlap: no two placements on one bench overlap in
//     [ActualStart, ActualStart+Duration) (different benches may overlap freely);
//   - priority respected: no placement delays a higher-priority slot;
//   - durations inviolable: every slot still runs for exactly its booked Duration;
//   - forward-only earliness: ActualStart ≥ WinStart, and WinStart only ratchets
//     later relative to the Input;
//   - ACTIVE/history untouched: only SCHEDULED slots appear in the Schedule;
//   - no time travel: no placement starts before Input.Now.
//
// Determinism (§4 invariant 6) is not checked here — it is asserted by the test
// harness running Reschedule twice and comparing. The engine calls this in tests
// over the full §11 matrix and may call it cheaply in prod; a violation is an
// engine bug, never a user-facing state.
func (s Schedule) CheckInvariants(in Input) error {
	panic("not implemented")
}
