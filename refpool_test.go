package refpool

import (
	"fmt"
	"math"
	"testing"
)

func ExamplePool() {
	type node struct {
		name string
	}

	p := New[node](4)

	original, _, fresh, _ := p.New()
	fmt.Println(fresh)
	p.Resolve(original).name = "root"

	alias := original
	p.Retain(alias)

	fmt.Println(p.Resolve(alias).name)
	p.Release(original)
	fmt.Println(p.Resolve(alias).name)

	p.Release(alias)
	reused, _, fresh, _ := p.New()
	fmt.Println(fresh, reused == original)

	// Output:
	// true
	// root
	// root
	// false true
}

func TestConstants(t *testing.T) {
	if siBits+ciBits != 32 {
		t.Fatalf("siBits + ciBits must equal 32, got %d", siBits+ciBits)
	}
	if chunkSize != 1<<siBits {
		t.Fatalf("chunkSize mismatch: got %d, want %d", chunkSize, 1<<siBits)
	}
	if siMask != uint16(chunkSize-1) {
		t.Fatalf("siMask mismatch: got %x, want %x", siMask, chunkSize-1)
	}
	if ciMask != Reference((1<<ciBits)-1) {
		t.Fatalf("ciMask mismatch: got %x, want %x", ciMask, (1<<ciBits)-1)
	}
}

func TestPackUnpackRoundTrip(t *testing.T) {
	maxCI := uint32(1<<ciBits) - 2 // because ci is stored as ci+1
	maxSI := uint16(1<<siBits) - 1

	cases := []struct {
		ci uint32
		si uint16
	}{
		{0, 0},
		{0, maxSI},
		{1, 0},
		{1, 1},
		{42, 7},
		{maxCI, 0},
		{maxCI, maxSI},
		{maxCI / 2, maxSI / 2},
	}

	for _, c := range cases {
		r := pack(c.ci, c.si)
		if r == 0 {
			t.Errorf("pack(%d, %d) produced invalid Reference 0", c.ci, c.si)
		}
		gotCI, gotSI := unpack(r)
		if gotCI != c.ci || gotSI != c.si {
			t.Errorf("round-trip failed for (ci=%d, si=%d): packed=%#x, unpacked=(%d, %d)", c.ci, c.si, r, gotCI, gotSI)
		}
	}
}

func TestPackNeverReturnsZero(t *testing.T) {
	// Every valid (ci, si) pair must pack to a non-zero Reference, because Reference(0) is reserved for "invalid / nil".
	if pack(0, 0) == 0 {
		t.Fatal("pack(0, 0) must not return 0")
	}
}

func TestPackSlotIndexMasked(t *testing.T) {
	// High bits of si beyond siBits must be ignored, not allowed to corrupt the chunk-index portion of the Reference.
	r1 := pack(7, 0x05)
	r2 := pack(7, 0x05|(1<<siBits)) // identical low 8 bits, extra high bit
	if r1 != r2 {
		t.Errorf("pack should mask si to siBits: r1=%#x, r2=%#x", r1, r2)
	}

	ci, si := unpack(r2)
	if ci != 7 || si != 0x05 {
		t.Errorf("masked unpack mismatch: got (ci=%d, si=%d), want (7, 5)", ci, si)
	}
}

func TestUnpackLayout(t *testing.T) {
	// Sanity check: chunk index (encoded as ci+1) lives in the low bits, slot index lives in the high bits.
	r := pack(0, 1)
	if r != (1<<ciBits)|1 {
		t.Errorf("unexpected layout: pack(0,1)=%#x, want %#x", r, (1<<ciBits)|1)
	}
}

func TestMaxValues(t *testing.T) {
	maxCI := uint32(1<<ciBits) - 2
	maxSI := uint16(1<<siBits) - 1

	r := pack(maxCI, maxSI)
	ci, si := unpack(r)
	if ci != maxCI || si != maxSI {
		t.Errorf("max round-trip failed: got (ci=%d, si=%d), want (%d, %d)", ci, si, maxCI, maxSI)
	}

	// Packing the max chunk index should keep ci+1 within ciBits.
	if Reference(maxCI+1)&^ciMask != 0 {
		t.Errorf("maxCI+1 overflows ciBits")
	}
}

func TestNewResolveReleaseAndReuse(t *testing.T) {
	p := New[string](0)

	r, _, fresh, _ := p.New()
	if !fresh {
		t.Fatal("first New returned fresh=false, want true")
	}
	if r != pack(0, 0) {
		t.Fatalf("first reference = %#x, want %#x", r, pack(0, 0))
	}

	*p.Resolve(r) = "live"
	if got := *p.Resolve(r); got != "live" {
		t.Fatalf("resolved value = %q, want %q", got, "live")
	}

	p.Release(r)
	if got := *p.Resolve(r); got != "" {
		t.Fatalf("released value = %q, want zero value", got)
	}

	reused, _, fresh, _ := p.New()
	if fresh {
		t.Fatal("New after Release returned fresh=true, want free-list reuse")
	}
	if reused != r {
		t.Fatalf("reused reference = %#x, want %#x", reused, r)
	}
	if got := *p.Resolve(reused); got != "" {
		t.Fatalf("reused value = %q, want zero value", got)
	}
}

func TestRetainRequiresMatchingReleases(t *testing.T) {
	p := New[int](0)

	r, _, _, _ := p.New()
	*p.Resolve(r) = 42
	p.Retain(r)
	p.Retain(r)

	p.Release(r)
	p.Release(r)
	if got := *p.Resolve(r); got != 42 {
		t.Fatalf("value after non-final releases = %d, want 42", got)
	}

	next, _, fresh, _ := p.New()
	if !fresh {
		t.Fatal("New before final Release reused a retained slot")
	}
	if next == r {
		t.Fatalf("New before final Release returned original reference %#x", r)
	}

	p.Release(r)
	if got := *p.Resolve(r); got != 0 {
		t.Fatalf("value after final Release = %d, want zero", got)
	}

	reused, _, fresh, _ := p.New()
	if fresh {
		t.Fatal("New after final Release returned fresh=true, want reuse")
	}
	if reused != r {
		t.Fatalf("reused reference = %#x, want %#x", reused, r)
	}
}

func TestPinPreventsReleaseAndRetainEffects(t *testing.T) {
	p := New[string](0)

	r, _, _, _ := p.New()
	*p.Resolve(r) = "pinned"
	p.Pin(r)
	p.Retain(r)
	p.Release(r)

	if got := *p.Resolve(r); got != "pinned" {
		t.Fatalf("pinned value after Retain/Release = %q, want %q", got, "pinned")
	}

	next, _, fresh, _ := p.New()
	if !fresh {
		t.Fatal("New after releasing pinned reference reused pinned slot")
	}
	if next == r {
		t.Fatalf("New after releasing pinned reference returned pinned reference %#x", r)
	}
	if p.chunks[0].slots[0].rc != 0 {
		t.Fatalf("pinned slot rc = %d, want 0", p.chunks[0].slots[0].rc)
	}
}

func TestRetainMaxRefCountPinsSlot(t *testing.T) {
	p := New[int](0)

	r, _, _, _ := p.New()
	ci, si := unpack(r)
	p.chunks[ci].slots[si].rc = math.MaxUint32 - 1

	p.Retain(r)
	if got := p.chunks[ci].slots[si].rc; got != 0 {
		t.Fatalf("rc after retaining max count = %d, want pinned rc 0", got)
	}

	p.Release(r)
	next, _, fresh, _ := p.New()
	if !fresh {
		t.Fatal("New after releasing auto-pinned reference reused pinned slot")
	}
	if next == r {
		t.Fatalf("New after releasing auto-pinned reference returned pinned reference %#x", r)
	}
}

func TestReleaseClearsPointerValues(t *testing.T) {
	p := New[*int](0)

	v := 42
	r, _, _, _ := p.New()
	*p.Resolve(r) = &v

	p.Release(r)
	if got := *p.Resolve(r); got != nil {
		t.Fatalf("released pointer value = %v, want nil", got)
	}
}

func TestAllocationAcrossChunkBoundary(t *testing.T) {
	p := New[int](chunkSize)

	var refs []Reference
	for range chunkSize + 2 {
		r, _, fresh, _ := p.New()
		if !fresh {
			t.Fatalf("New returned fresh=false while no slots had been released")
		}
		refs = append(refs, r)
	}

	if refs[chunkSize-1] != pack(0, chunkSize-1) {
		t.Fatalf("last first-chunk reference = %#x, want %#x", refs[chunkSize-1], pack(0, chunkSize-1))
	}
	if refs[chunkSize] != pack(1, 0) {
		t.Fatalf("first second-chunk reference = %#x, want %#x", refs[chunkSize], pack(1, 0))
	}
	if refs[chunkSize+1] != pack(1, 1) {
		t.Fatalf("second second-chunk reference = %#x, want %#x", refs[chunkSize+1], pack(1, 1))
	}
	if len(p.chunks) != 2 {
		t.Fatalf("len(p.chunks) = %d, want 2", len(p.chunks))
	}
	if p.index != 1 {
		t.Fatalf("p.index = %d, want 1", p.index)
	}
}

func TestNewStoresBaseChunks(t *testing.T) {
	cases := []struct {
		preAlloc int
		want     uint32
	}{
		{0, 1},
		{1, 1},
		{chunkSize, 1},
		{chunkSize + 1, 2},
		{2 * chunkSize, 2},
	}

	for _, c := range cases {
		p := New[int](c.preAlloc)
		if p.baseChunks != c.want {
			t.Errorf("New(%d).baseChunks = %d, want %d", c.preAlloc, p.baseChunks, c.want)
		}
		if len(p.chunks) != int(c.want) {
			t.Errorf("New(%d) allocated %d chunks, want %d", c.preAlloc, len(p.chunks), c.want)
		}
	}
}

func TestResetKeepsAllocatedChunks(t *testing.T) {
	p := New[int](1)

	var last Reference
	for range chunkSize + 1 {
		r, _, _, _ := p.New()
		*p.Resolve(r) = 42
		last = r
	}

	if len(p.chunks) != 2 {
		t.Fatalf("len(p.chunks) before Reset = %d, want 2", len(p.chunks))
	}

	p.Release(last)
	p.Reset()

	if len(p.chunks) != 2 {
		t.Fatalf("len(p.chunks) after Reset = %d, want 2", len(p.chunks))
	}
	if p.index != 0 {
		t.Fatalf("p.index after Reset = %d, want 0", p.index)
	}
	if p.free != 0 {
		t.Fatalf("p.free after Reset = %d, want 0", p.free)
	}
	if p.chunks[0].next != 0 {
		t.Fatalf("p.chunks[0].next after Reset = %d, want 0", p.chunks[0].next)
	}

	r, _, fresh, _ := p.New()
	if !fresh {
		t.Fatal("New after Reset reused free-list slot, want fresh allocation from reset chunk")
	}
	if r != pack(0, 0) {
		t.Fatalf("first reference after Reset = %#x, want %#x", r, pack(0, 0))
	}
	if got := *p.Resolve(r); got != 0 {
		t.Fatalf("first value after Reset = %d, want zero", got)
	}
}

func TestResetFullShrinksToBaseChunks(t *testing.T) {
	p := New[int](chunkSize + 1)

	for range (2 * chunkSize) + 1 {
		r, _, _, _ := p.New()
		*p.Resolve(r) = 42
	}

	if p.baseChunks != 2 {
		t.Fatalf("p.baseChunks = %d, want 2", p.baseChunks)
	}
	if len(p.chunks) != 3 {
		t.Fatalf("len(p.chunks) before ResetFull = %d, want 3", len(p.chunks))
	}

	p.ResetFull()

	if len(p.chunks) != int(p.baseChunks) {
		t.Fatalf("len(p.chunks) after ResetFull = %d, want %d", len(p.chunks), p.baseChunks)
	}
	if p.index != 0 {
		t.Fatalf("p.index after ResetFull = %d, want 0", p.index)
	}
	if p.free != 0 {
		t.Fatalf("p.free after ResetFull = %d, want 0", p.free)
	}

	r, _, fresh, _ := p.New()
	if !fresh {
		t.Fatal("New after ResetFull reused free-list slot, want fresh allocation from reset chunk")
	}
	if r != pack(0, 0) {
		t.Fatalf("first reference after ResetFull = %#x, want %#x", r, pack(0, 0))
	}
	if got := *p.Resolve(r); got != 0 {
		t.Fatalf("first value after ResetFull = %d, want zero", got)
	}
}

func TestResetFullClearsDroppedChunkPointers(t *testing.T) {
	p := New[*int](1)

	v := 42
	for range chunkSize + 1 {
		r, _, _, _ := p.New()
		*p.Resolve(r) = &v
	}

	if len(p.chunks) != 2 {
		t.Fatalf("len(p.chunks) before ResetFull = %d, want 2", len(p.chunks))
	}

	p.ResetFull()

	chunksBackingArray := p.chunks[:cap(p.chunks)]
	for i := int(p.baseChunks); i < len(chunksBackingArray); i++ {
		if chunksBackingArray[i] != nil {
			t.Fatalf("dropped chunk pointer at backing index %d was not cleared", i)
		}
	}
}
