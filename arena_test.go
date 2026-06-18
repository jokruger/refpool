package refpool

import (
	"math"
	"testing"
)

func TestArena_NewResolveReleaseAndReuse(t *testing.T) {
	p := Type(0)
	a := NewArena(With[string](p, 0))

	r, _, ok := a.New(p)
	if !ok {
		t.Fatal("first New returned ok=false, want true")
	}
	if r != pack(0, 0) {
		t.Fatalf("first reference = %#x, want %#x", r, pack(0, 0))
	}

	*Resolve[string](a, p, r) = "live"
	if got := *Resolve[string](a, p, r); got != "live" {
		t.Fatalf("resolved value = %q, want %q", got, "live")
	}

	a.Release(p, r)
	if got := *Resolve[string](a, p, r); got != "" {
		t.Fatalf("released value = %q, want zero value", got)
	}

	reused, _, ok := a.New(p)
	if !ok {
		t.Fatal("New after Release returned ok=false, want true")
	}
	if reused != r {
		t.Fatalf("reused reference = %#x, want %#x", reused, r)
	}
	if got := *Resolve[string](a, p, r); got != "" {
		t.Fatalf("reused value = %q, want zero value", got)
	}
}

// TestArena_PreAllocMultipleChunks verifies that pre-allocating more than chunkSize
// slots makes all of them accessible. This exercises the grow chaining fix.
func TestArena_PreAllocMultipleChunks(t *testing.T) {
	p := Type(0)
	total := chunkSize*3 + 1 // spans 4 chunks
	a := NewArena(With[int](p, total))

	allocated, free := a.Stats(p)
	if allocated != chunkSize*4 {
		t.Fatalf("allocated = %d, want %d", allocated, chunkSize*4)
	}
	if free != allocated {
		t.Fatalf("free = %d, want %d (all pre-allocated slots should be free)", free, allocated)
	}

	// Allocate every slot and verify each succeeds.
	refs := make([]Reference, 0, allocated)
	for i := range allocated {
		r, v, ok := a.New(p)
		if !ok {
			t.Fatalf("New #%d returned ok=false", i)
		}
		*v.(*int) = i
		refs = append(refs, r)
	}

	// Verify values survive.
	for i, r := range refs {
		if got := *Resolve[int](a, p, r); got != i {
			t.Fatalf("refs[%d] value = %d, want %d", i, got, i)
		}
	}
}

// TestArena_RetainRequiresMatchingReleases verifies reference-count semantics.
func TestArena_RetainRequiresMatchingReleases(t *testing.T) {
	p := Type(0)
	a := NewArena(With[int](p, 0))

	r, _, _ := a.New(p)
	*Resolve[int](a, p, r) = 99
	a.Retain(p, r)
	a.Retain(p, r)

	// Two of three refs released — value must survive.
	a.Release(p, r)
	a.Release(p, r)
	if got := *Resolve[int](a, p, r); got != 99 {
		t.Fatalf("value after two releases = %d, want 99", got)
	}

	// The next New must not reuse r (it is still alive).
	next, _, _ := a.New(p)
	if next == r {
		t.Fatalf("New before final Release returned original reference %#x", r)
	}

	// Final release — slot goes to free-list and is zeroed.
	a.Release(p, r)
	if got := *Resolve[int](a, p, r); got != 0 {
		t.Fatalf("value after final Release = %d, want 0", got)
	}

	reused, _, _ := a.New(p)
	if reused != r {
		t.Fatalf("reused reference = %#x, want %#x", reused, r)
	}
}

// TestArena_PinPreventsRetainAndReleaseEffects verifies that pinned slots are
// not reference-counted and are not returned to the free-list on Release.
func TestArena_PinPreventsRetainAndReleaseEffects(t *testing.T) {
	p := Type(0)
	a := NewArena(With[string](p, 0))

	r, _, _ := a.New(p)
	*Resolve[string](a, p, r) = "pinned"
	a.Pin(p, r)
	a.Retain(p, r)  // no-op on pinned
	a.Release(p, r) // no-op on pinned

	if got := *Resolve[string](a, p, r); got != "pinned" {
		t.Fatalf("pinned value after Retain/Release = %q, want %q", got, "pinned")
	}

	// Next allocation must not reuse the pinned slot.
	next, _, _ := a.New(p)
	if next == r {
		t.Fatalf("New returned pinned reference %#x", r)
	}

	// Confirm rc is still 0 (pinned).
	ci, si := unpack(r)
	if rc := a.pools[p].chunks[ci].slots[si].rc; rc != 0 {
		t.Fatalf("pinned slot rc = %d, want 0", rc)
	}
}

// TestArena_RetainMaxRefCountPinsSlot verifies that a slot is auto-pinned when
// the reference count would overflow math.MaxUint32.
func TestArena_RetainMaxRefCountPinsSlot(t *testing.T) {
	p := Type(0)
	a := NewArena(With[int](p, 0))

	r, _, _ := a.New(p)
	ci, si := unpack(r)
	a.pools[p].chunks[ci].slots[si].rc = math.MaxUint32 - 1

	a.Retain(p, r) // brings rc to MaxUint32 — triggers auto-pin

	if rc := a.pools[p].chunks[ci].slots[si].rc; rc != 0 {
		t.Fatalf("rc after overflow Retain = %d, want 0 (pinned)", rc)
	}
}

// TestArena_ZeroOnReleaseFalse verifies that WithZeroOnRelease(false) skips zeroing.
func TestArena_ZeroOnReleaseFalse(t *testing.T) {
	p := Type(0)
	a := NewArena(WithZeroOnRelease(false), With[int](p, 0))

	r, _, _ := a.New(p)
	*Resolve[int](a, p, r) = 77
	a.Release(p, r)

	if got := *Resolve[int](a, p, r); got != 77 {
		t.Fatalf("value after Release (zeroOnRelease=false) = %d, want 77", got)
	}
}

// TestArena_Stats verifies that Stats returns correct allocated and free counts.
func TestArena_Stats(t *testing.T) {
	p := Type(0)
	a := NewArena(With[int](p, 0))

	allocated, free := a.Stats(p)
	if allocated != chunkSize {
		t.Fatalf("initial allocated = %d, want %d", allocated, chunkSize)
	}
	if free != chunkSize {
		t.Fatalf("initial free = %d, want %d", free, chunkSize)
	}

	r, _, _ := a.New(p)
	_, free = a.Stats(p)
	if free != chunkSize-1 {
		t.Fatalf("free after one New = %d, want %d", free, chunkSize-1)
	}

	a.Release(p, r)
	_, free = a.Stats(p)
	if free != chunkSize {
		t.Fatalf("free after Release = %d, want %d", free, chunkSize)
	}
}

// TestArena_AutoGrow verifies that the pool grows automatically when all slots are used.
func TestArena_AutoGrow(t *testing.T) {
	p := Type(0)
	a := NewArena(With[int](p, 0))

	// Exhaust the first chunk.
	refs := make([]Reference, chunkSize)
	for i := range chunkSize {
		r, _, ok := a.New(p)
		if !ok {
			t.Fatalf("New #%d returned ok=false", i)
		}
		refs[i] = r
	}

	allocated, _ := a.Stats(p)
	if allocated != chunkSize {
		t.Fatalf("allocated before grow = %d, want %d", allocated, chunkSize)
	}

	// One more allocation must trigger a grow.
	r, _, ok := a.New(p)
	if !ok {
		t.Fatal("New after chunk exhaustion returned ok=false")
	}

	allocated, _ = a.Stats(p)
	if allocated != chunkSize*2 {
		t.Fatalf("allocated after grow = %d, want %d", allocated, chunkSize*2)
	}

	// The new reference should be in the second chunk.
	ci, _ := unpack(r)
	if ci != 1 {
		t.Fatalf("new reference chunk index = %d, want 1", ci)
	}
	_ = refs
}

// TestArena_Reset verifies that Reset makes all pre-allocated slots available again.
func TestArena_Reset(t *testing.T) {
	p := Type(0)
	a := NewArena(With[int](p, chunkSize*2))

	// Allocate all pre-allocated slots.
	total := chunkSize * 2
	for i := range total {
		_, _, ok := a.New(p)
		if !ok {
			t.Fatalf("New #%d returned ok=false before Reset", i)
		}
	}

	_, free := a.Stats(p)
	if free != 0 {
		t.Fatalf("free before Reset = %d, want 0", free)
	}

	a.Reset(p)

	allocated, free := a.Stats(p)
	if allocated != total {
		t.Fatalf("allocated after Reset = %d, want %d", allocated, total)
	}
	if free != total {
		t.Fatalf("free after Reset = %d, want %d", free, total)
	}

	// Extra chunks allocated after init should be dropped.
	// (No extra chunks were allocated here, so len(chunks) stays at 2.)
	if got := len(a.pools[p].chunks); got != 2 {
		t.Fatalf("chunks after Reset = %d, want 2", got)
	}
}

// TestArena_MultipleTypes verifies that two pool types are fully independent.
func TestArena_MultipleTypes(t *testing.T) {
	pInt := Type(0)
	pStr := Type(1)
	a := NewArena(With[int](pInt, 0), With[string](pStr, 0))

	ri, vi, ok := a.New(pInt)
	if !ok {
		t.Fatal("New int returned ok=false")
	}
	*vi.(*int) = 42

	rs, vs, ok := a.New(pStr)
	if !ok {
		t.Fatal("New string returned ok=false")
	}
	*vs.(*string) = "hello"

	if got := *Resolve[int](a, pInt, ri); got != 42 {
		t.Fatalf("int value = %d, want 42", got)
	}
	if got := *Resolve[string](a, pStr, rs); got != "hello" {
		t.Fatalf("string value = %q, want %q", got, "hello")
	}

	// Release one type; the other must be unaffected.
	a.Release(pInt, ri)
	if got := *Resolve[string](a, pStr, rs); got != "hello" {
		t.Fatalf("string value after releasing int = %q, want %q", got, "hello")
	}
}

// TestArena_ReleaseOnPinnedIsNoop verifies Release is a no-op when rc == 0 (pinned).
func TestArena_ReleaseOnPinnedIsNoop(t *testing.T) {
	p := Type(0)
	a := NewArena(With[int](p, 0))

	r, _, _ := a.New(p)
	*Resolve[int](a, p, r) = 7
	a.Pin(p, r)

	a.Release(p, r) // should not add to free-list

	// Verify the value is unchanged.
	if got := *Resolve[int](a, p, r); got != 7 {
		t.Fatalf("value after Release on pinned = %d, want 7", got)
	}

	// Verify the slot is not in the free-list by consuming the rest and checking
	// that r is never handed back.
	for range chunkSize - 1 {
		next, _, ok := a.New(p)
		if !ok {
			break
		}
		if next == r {
			t.Fatalf("New returned pinned reference %#x", r)
		}
	}
}
