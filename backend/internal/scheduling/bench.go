package scheduling

// Bench is one interchangeable machine within a pool. A pool's queue is single
// file but fans out across its benches based on availability, so the no-overlap
// invariant is per bench, not per pool, and a delay on one bench need not delay
// the next person if another bench is free (§1.4, §4, §7.2).
//
// A Bench carries no schedule of its own: occupancy is derived from the slots
// placed on it. The engine only needs the set of benches it may place onto.
type Bench struct {
	ID     BenchID
	PoolID PoolID
}
