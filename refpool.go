// Package refpool implements a pool of reference-counted values. It is designed for use in arenas, where resources are
// allocated and released in bulk. The pool allows efficient allocation and deallocation of resources, while minimizing
// memory fragmentation and overhead. It also supports pinning resources to prevent them from being reclaimed until the
// arena is reset (resources reached max ref count are automatically pinned).
//
// The client must guarantee the correct flow of New/Retain/Resolve/Release/Pin/Reset calls:
// - Retain must be called when logical copy of the reference is created,
// - each Retain eventually must be matched with corresponding Release (exception are pinned resources),
// - Pin must be called when client cannot guarantee the correct flow of Retain/Release calls,
// - Pointer to the actual resource received with Resolve must be treated as temporary and not stored anywhere,
// - No resources should be used after Reset until they are re-allocated with New.
package refpool

import (
	"math"
)

// Reference is a handle to a value in the pool. 0 corresponds to invalid / nil reference.
type Reference uint64

// Pool of reference-counted values.
type Pool[T any] struct {
	chunks        []*chunk[T] // allocated chunks
	current       *chunk[T]   // currently active chunk (same as chunks[index])
	free          Reference   // free-list head
	index         uint32      // current chunk in use
	baseChunks    uint32      // number of pre-allocated chunks calculated when new Pool was created
	zero          T           // zero value of T for value reset
	zeroOnRelease bool        // whether Release should reset value to zero
	zeroOnReset   bool        // whether Reset should reset values to zero
}

// Slot index occupies high bits of a reference. Chunk index occupies low bits of a reference and is encoded as
// actual value + 1 which makes Reference = 0 invalid (not pointing to any slot).
const chunkSize = 256                // number of slots per chunk
const minChunks = 32                 // min size of chunks slice
const maxChunks = math.MaxUint32 - 1 // max number of chunks

// Chunk of slots for storing values.
type chunk[T any] struct {
	slots [chunkSize]slot[T] // fixed-size array of slots
	next  uint32             // next free slot index in the chunk (if all slots are used, next is set to chunkSize)
}

// Single value slot.
type slot[T any] struct {
	value    T         // value stored in the slot
	nextFree Reference // next unused reference in free-list
	rc       uint32    // reference count (0 = slot is not used yet, or it is used but should not be reference counted)
}

// Assemble a reference from chunk index and slot index.
func pack(ci uint32, si uint32) Reference {
	return Reference(ci+1) | Reference(si)<<32
}

// Disassemble a reference into chunk index and slot index. Reference must be valid (not zero).
func unpack(r Reference) (uint32, uint32) {
	return uint32(r) - 1, uint32(r >> 32)
}

// New creates a new typed pool with at least `preAlloc` pre-allocated values.
func New[T any](preAlloc int) *Pool[T] {
	// calc how many chunks we need to pre-allocate
	cs := preAlloc / chunkSize
	if preAlloc%chunkSize != 0 {
		cs++
	}
	cs = max(1, cs)

	// allocate chunks slice with exact initial length and extra buffer for future growth
	p := &Pool[T]{chunks: make([]*chunk[T], cs, max(cs, minChunks))}

	// pre-allocate chunks
	for i := range p.chunks {
		p.chunks[i] = &chunk[T]{}
	}

	// store baseChunks for future use in ResetFull
	p.baseChunks = uint32(cs)
	p.current = p.chunks[0]
	p.zeroOnRelease = true
	p.zeroOnReset = true

	return p
}

// New creates a new (or re-use free) reference to a value in the pool. The value is not initialized and should be set
// by the caller. Returns the reference, value pointer, and flag indicating whether allocation was successful.
func (p *Pool[T]) New() (Reference, *T, bool) {
	if r, v, ok := p.newFast(); ok {
		return r, v, true
	}

	return p.newSlow()
}

// newFast serves the hot path: free-list reuse and allocation from current chunk.
func (p *Pool[T]) newFast() (Reference, *T, bool) {
	// re-use free slot if possible
	if p.free != 0 {
		r := p.free
		ci, si := unpack(r)
		s := &p.chunks[ci].slots[si]
		p.free = s.nextFree // update free-list head
		s.rc = 1            // reset ref count to 1
		return r, &s.value, true
	}

	// use next slot in current chunk if possible
	c := p.current
	if c.next < chunkSize {
		si := c.next
		c.slots[si].rc = 1 // reset ref count to 1
		c.next++
		return pack(p.index, si), &c.slots[si].value, true
	}

	return 0, nil, false
}

// newSlow handles chunk rollover, growth and overflow checks.
func (p *Pool[T]) newSlow() (Reference, *T, bool) {
	// next chunk
	p.index++

	// can use pre-allocated chunk?
	if int(p.index) < len(p.chunks) {
		c := p.chunks[p.index]
		p.current = c
		c.slots[0].rc = 1 // reset ref count to 1
		c.next = 1
		return pack(p.index, 0), &c.slots[0].value, true
	}

	// can allocate new chunk?
	if p.index < maxChunks {
		c := &chunk[T]{next: 1}
		p.current = c
		c.slots[0].rc = 1 // reset ref count to 1
		p.chunks = append(p.chunks, c)
		return pack(p.index, 0), &c.slots[0].value, true
	}

	return 0, nil, false
}

// Pin marks the resource as arena-live (provided reference must be valid). A pinned resource is not reclaimed when its
// reference count reaches zero. It remains owned by the allocator and is released on Reset only.
func (p *Pool[T]) Pin(r Reference) {
	ci, si := unpack(r)
	p.chunks[ci].slots[si].rc = 0
}

// Retain increments the reference count of the resource (provided reference must be valid). If the resource is pinned,
// it does nothing since pinned resources are not reference-counted. If the reference count reaches the maximum value
// it pins the resource to prevent overflow and potential bugs.
func (p *Pool[T]) Retain(r Reference) {
	ci, si := unpack(r)
	s := &p.chunks[ci].slots[si]
	if s.rc > 0 {
		// ref count only if resource is not pinned
		s.rc++
		if s.rc == math.MaxUint32 {
			// if ref count reached max value, pin the resource to prevent overflow and potential bugs
			s.rc = 0
		}
	}
}

// Release decrements the reference count of the resource (provided reference must be valid). If the resource is pinned,
// it does nothing since pinned resources are not reference-counted. If the reference count reaches zero, it adds the
// resource to the free-list for future reuse.
func (p *Pool[T]) Release(r Reference) {
	ci, si := unpack(r)
	s := &p.chunks[ci].slots[si]
	if s.rc > 0 {
		// ref count only if resource is not pinned
		s.rc--
		if s.rc == 0 {
			// add to free-list if ref count reached zero
			s.nextFree = p.free
			if p.zeroOnRelease {
				s.value = p.zero // reset value to zero for safety and GC
			}
			p.free = r
		}
	}
}

// SetZeroOnRelease configures whether Release should reset value to the zero value of T when ref count reaches zero.
// The default is true.
func (p *Pool[T]) SetZeroOnRelease(enabled bool) *Pool[T] {
	p.zeroOnRelease = enabled
	return p
}

// SetZeroOnReset configures whether Reset should reset values to the zero value of T.
// The default is true.
func (p *Pool[T]) SetZeroOnReset(enabled bool) *Pool[T] {
	p.zeroOnReset = enabled
	return p
}

// Resolve returns a pointer to the value associated with the reference (provided reference must be valid). The returned
// pointer is temporary and should not be stored anywhere. The caller should use Resolve only to access the value of the
// resource, but not to manage its lifetime.
func (p *Pool[T]) Resolve(r Reference) *T {
	ci, si := unpack(r)
	return &p.chunks[ci].slots[si].value
}

// Reset clears all allocated values and makes the pool ready for the next cycle. It keeps all currently allocated
// resources for reuse.
func (p *Pool[T]) Reset() {
	// set values to zero to release potentially held resources when enabled
	for ci := uint32(0); ci <= p.index; ci++ {
		c := p.chunks[ci]
		if p.zeroOnReset {
			for i := uint32(0); i < c.next; i++ {
				c.slots[i].value = p.zero
			}
		}
		c.next = 0 // reset chunk to initial state
	}

	// reset pool state
	p.index = 0
	p.current = p.chunks[0]
	p.free = 0
}

// ResetFull clears all allocated values and makes the pool ready for the next cycle. It drops resources allocated
// after pool creation, leaving only the base pre-allocated resources.
func (p *Pool[T]) ResetFull() {
	// reset tail pointers to nil to release resources allocated after pool creation
	for i := p.baseChunks; i < uint32(len(p.chunks)); i++ {
		p.chunks[i] = nil
	}
	p.chunks = p.chunks[:p.baseChunks]     // drop all chunks allocated after pool creation (if any)
	p.index = min(p.index, p.baseChunks-1) // adjust index so Reset can safely clear all remaining chunks
	p.current = p.chunks[p.index]
	p.Reset()
}
