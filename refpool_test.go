package refpool

import (
	"math"
	"testing"
)

func TestPackUnpackRoundTrip(t *testing.T) {
	cases := []struct {
		ci uint32
		si uint32
	}{
		{0, 0},
		{0, 255},
		{0, 256},
		{0, 1234567890},
		{0, 4294967295},
		{1, 0},
		{1, 1},
		{42, 7},
		{1234567890, 0},
		{4294967294, 0},
		{4294967294, 4294967295},
		{4294967295 / 2, 4294967295 / 2},
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

func TestPackUsesExpectedLayout(t *testing.T) {
	r := pack(0, 1)
	want := Reference(1)<<32 | 1
	if r != want {
		t.Fatalf("pack(0, 1) = %#x, want %#x", r, want)
	}

	ci, si := unpack(r)
	if ci != 0 || si != 1 {
		t.Fatalf("unpack(pack(0, 1)) = (%d, %d), want (0, 1)", ci, si)
	}
}

func TestPackMaxValidValues(t *testing.T) {
	ci := uint32(maxChunks - 1)
	si := uint32(chunkSize - 1)

	r := pack(ci, si)
	if r == 0 {
		t.Fatal("pack(max valid values) must not return 0")
	}

	gotCI, gotSI := unpack(r)
	if gotCI != ci || gotSI != si {
		t.Fatalf("round-trip failed for max valid values: packed=%#x, unpacked=(%d, %d)", r, gotCI, gotSI)
	}
}

func TestNewResolveReleaseAndReuse(t *testing.T) {
	p := New[string](0, nil)

	r, _, ok := p.New()
	if !ok {
		t.Fatal("first New returned ok=false, want true")
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

	reused, _, ok := p.New()
	if !ok {
		t.Fatal("New after Release returned ok=false, want true")
	}
	if reused != r {
		t.Fatalf("reused reference = %#x, want %#x", reused, r)
	}
	if got := *p.Resolve(reused); got != "" {
		t.Fatalf("reused value = %q, want zero value", got)
	}
}

func TestRetainRequiresMatchingReleases(t *testing.T) {
	p := New[int](0, nil)

	r, _, _ := p.New()
	*p.Resolve(r) = 42
	p.Retain(r)
	p.Retain(r)

	p.Release(r)
	p.Release(r)
	if got := *p.Resolve(r); got != 42 {
		t.Fatalf("value after non-final releases = %d, want 42", got)
	}

	next, _, _ := p.New()
	if next == r {
		t.Fatalf("New before final Release returned original reference %#x", r)
	}

	p.Release(r)
	if got := *p.Resolve(r); got != 0 {
		t.Fatalf("value after final Release = %d, want zero", got)
	}

	reused, _, _ := p.New()
	if reused != r {
		t.Fatalf("reused reference = %#x, want %#x", reused, r)
	}
}

func TestPinPreventsReleaseAndRetainEffects(t *testing.T) {
	p := New[string](0, nil)

	r, _, _ := p.New()
	*p.Resolve(r) = "pinned"
	p.Pin(r)
	p.Retain(r)
	p.Release(r)

	if got := *p.Resolve(r); got != "pinned" {
		t.Fatalf("pinned value after Retain/Release = %q, want %q", got, "pinned")
	}

	next, _, _ := p.New()
	if next == r {
		t.Fatalf("New after releasing pinned reference returned pinned reference %#x", r)
	}
	if p.chunks[0].slots[0].rc != 0 {
		t.Fatalf("pinned slot rc = %d, want 0", p.chunks[0].slots[0].rc)
	}
}

func TestRetainMaxRefCountPinsSlot(t *testing.T) {
	p := New[int](0, nil)

	r, _, _ := p.New()
	ci, si := unpack(r)
	p.chunks[ci].slots[si].rc = math.MaxUint32 - 1

	p.Retain(r)
	if got := p.chunks[ci].slots[si].rc; got != 0 {
		t.Fatalf("rc after retaining max count = %d, want pinned rc 0", got)
	}

	p.Release(r)
	next, _, _ := p.New()
	if next == r {
		t.Fatalf("New after releasing auto-pinned reference returned pinned reference %#x", r)
	}
}

func TestReleaseClearsPointerValues(t *testing.T) {
	p := New[*int](0, nil)

	v := 42
	r, _, _ := p.New()
	*p.Resolve(r) = &v

	p.Release(r)
	if got := *p.Resolve(r); got != nil {
		t.Fatalf("released pointer value = %v, want nil", got)
	}
}

func TestReleaseCanKeepValueWhenZeroOnReleaseDisabled(t *testing.T) {
	p := New[*int](0, nil).SetZeroOnRelease(false)

	v := 42
	r, _, _ := p.New()
	*p.Resolve(r) = &v

	p.Release(r)
	if got := *p.Resolve(r); got != &v {
		t.Fatalf("released pointer value = %v, want %v", got, &v)
	}
}

func TestAllocationAcrossChunkBoundary(t *testing.T) {
	p := New[int](chunkSize, nil)

	var refs []Reference
	for range chunkSize + 2 {
		r, _, ok := p.New()
		if !ok {
			t.Fatal("New returned ok=false before reaching capacity")
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

func TestReuseFollowsFreeListLIFO(t *testing.T) {
	p := New[int](0, nil)

	r1, _, ok := p.New()
	if !ok {
		t.Fatal("first New returned ok=false, want true")
	}
	r2, _, ok := p.New()
	if !ok {
		t.Fatal("second New returned ok=false, want true")
	}
	r3, _, ok := p.New()
	if !ok {
		t.Fatal("third New returned ok=false, want true")
	}

	p.Release(r1)
	p.Release(r2)
	p.Release(r3)

	reused1, _, ok := p.New()
	if !ok {
		t.Fatal("first reuse New returned ok=false, want true")
	}
	reused2, _, ok := p.New()
	if !ok {
		t.Fatal("second reuse New returned ok=false, want true")
	}
	reused3, _, ok := p.New()
	if !ok {
		t.Fatal("third reuse New returned ok=false, want true")
	}

	if reused1 != r3 || reused2 != r2 || reused3 != r1 {
		t.Fatalf(
			"reuse order mismatch: got [%#x %#x %#x], want [%#x %#x %#x]",
			reused1, reused2, reused3, r3, r2, r1,
		)
	}
}

func TestNewSlowOverflowReturnsFalse(t *testing.T) {
	p := &Pool[int]{
		chunks: []*chunk[int]{{}},
		index:  maxChunks,
	}

	r, v, ok := p.newSlow()
	if ok {
		t.Fatal("newSlow returned ok=true at overflow boundary, want false")
	}
	if r != 0 {
		t.Fatalf("overflow reference = %#x, want 0", r)
	}
	if v != nil {
		t.Fatalf("overflow value pointer = %v, want nil", v)
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
		p := New[int](c.preAlloc, nil)
		if p.baseChunks != c.want {
			t.Errorf("New(%d).baseChunks = %d, want %d", c.preAlloc, p.baseChunks, c.want)
		}
		if len(p.chunks) != int(c.want) {
			t.Errorf("New(%d) allocated %d chunks, want %d", c.preAlloc, len(p.chunks), c.want)
		}
	}
}

func TestResetKeepsAllocatedChunks(t *testing.T) {
	p := New[int](1, nil)

	var last Reference
	for range chunkSize + 1 {
		r, _, _ := p.New()
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

	r, _, _ := p.New()
	if r != pack(0, 0) {
		t.Fatalf("first reference after Reset = %#x, want %#x", r, pack(0, 0))
	}
	if got := *p.Resolve(r); got != 0 {
		t.Fatalf("first value after Reset = %d, want zero", got)
	}
}

func TestResetFullShrinksToBaseChunks(t *testing.T) {
	p := New[int](chunkSize+1, nil)

	for range (2 * chunkSize) + 1 {
		r, _, _ := p.New()
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

	r, _, _ := p.New()
	if r != pack(0, 0) {
		t.Fatalf("first reference after ResetFull = %#x, want %#x", r, pack(0, 0))
	}
	if got := *p.Resolve(r); got != 0 {
		t.Fatalf("first value after ResetFull = %d, want zero", got)
	}
}

func TestResetFullClearsDroppedChunkPointers(t *testing.T) {
	p := New[*int](1, nil)

	v := 42
	for range chunkSize + 1 {
		r, _, _ := p.New()
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

func TestResetCanKeepValuesWhenZeroOnResetDisabled(t *testing.T) {
	p := New[*int](1, nil).SetZeroOnReset(false)

	v := 42
	r, _, _ := p.New()
	*p.Resolve(r) = &v

	p.Reset()

	r2, _, ok := p.New()
	if !ok {
		t.Fatal("New after Reset returned ok=false, want true")
	}
	if r2 != pack(0, 0) {
		t.Fatalf("first reference after Reset = %#x, want %#x", r2, pack(0, 0))
	}
	if got := *p.Resolve(r2); got != &v {
		t.Fatalf("value after Reset with zeroing disabled = %v, want %v", got, &v)
	}
}
