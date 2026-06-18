package refpool

import (
	"math"
	"unsafe"
)

type Arena struct {
	pools         [256]typePool
	zeroOnRelease bool // whether Release should reset value to zero
	zeroOnReset   bool // whether Reset should reset values to zero
}

type typePool struct {
	chunks  []*typeChunk // allocated chunks
	current *typeChunk   // currently active chunk (same as chunks[index])
	free    Reference    // free-list head
	index   uint32       // current chunk index for linear bump allocation
	base    uint32       // number of pre-allocated chunks calculated when new pool was created

	alloc func() unsafe.Pointer    // allocate typed buffer and return base pointer
	reset func(ptr unsafe.Pointer) // reset a value to zero
	esz   uintptr                  // element size for pointer arithmetic
}

type typeChunk struct {
	base  unsafe.Pointer      // base pointer to first value in typed buffer
	slots [chunkSize]typeSlot // fixed-size array of slots
	next  uint32              // next uninitialized slot for linear bump allocation
}

type typeSlot struct {
	nextFree Reference // next unused reference in free-list
	rc       uint32    // reference count (0 = slot is not used yet, or it is used but should not be reference counted)
}

func allocBuffer[T any]() unsafe.Pointer {
	type buffer = [chunkSize]T
	b := &buffer{}
	return unsafe.Pointer(&b[0])
}

func resetPtr[T any](ptr unsafe.Pointer) {
	var zero T
	*(*T)(ptr) = zero
}

func NewArena(zeroOnRelease, zeroOnReset bool, types ...func(*Arena)) *Arena {
	a := &Arena{zeroOnRelease: zeroOnRelease, zeroOnReset: zeroOnReset}
	for _, wt := range types {
		wt(a)
	}
	return a
}

func With[T any](pool Type, preAlloc int) func(*Arena) {
	return func(a *Arena) {
		p := &a.pools[pool]
		p.alloc = allocBuffer[T]
		p.reset = resetPtr[T]

		var zero T
		p.esz = unsafe.Sizeof(zero)

		// calc how many chunks we need to pre-allocate
		cs := preAlloc / chunkSize
		if preAlloc%chunkSize != 0 {
			cs++
		}
		cs = max(1, cs)

		// allocate chunks slice with exact initial length and extra buffer for future growth
		p.base = uint32(cs)
		p.chunks = make([]*typeChunk, cs, max(cs, minChunks))

		// pre-allocate chunks
		for i := range p.chunks {
			p.chunks[i] = &typeChunk{base: p.alloc()}
		}
		p.current = p.chunks[0]
	}
}

// Stats returns the current pool stats.
func (a *Arena) Stats(pool Type) (allocated, used, free int) {
	p := &a.pools[pool]

	allocated = len(p.chunks) * chunkSize
	used = int(p.index)*chunkSize + int(p.current.next)
	free = 0

	i := p.free
	for i != 0 {
		free++
		ci, si := unpack(i)
		i = p.chunks[ci].slots[si].nextFree
	}

	return allocated, used, free
}

// New creates a new (or re-use free) reference to a value in the pool. The value is not initialized and should be set
// by the caller. Returns the reference, value pointer and flag indicating whether allocation was successful.
func (a *Arena) New(pool Type) (Reference, unsafe.Pointer, bool) {
	p := &a.pools[pool]
	if r, v, ok := a.newFast(p); ok {
		return r, v, true
	}
	return a.newSlow(p)
}

func (a *Arena) newFast(p *typePool) (Reference, unsafe.Pointer, bool) {
	// re-use free slot if possible
	if p.free != 0 {
		r := p.free
		ci, si := unpack(r)
		c := p.chunks[ci]
		s := &c.slots[si]
		p.free = s.nextFree // update free-list head
		s.rc = 1            // reset ref count to 1
		v := unsafe.Add(c.base, uintptr(si)*p.esz)
		return r, v, true
	}

	// use next slot in current chunk if possible
	c := p.current
	if c.next < chunkSize {
		si := c.next
		c.slots[si].rc = 1 // reset ref count to 1
		c.next++
		v := unsafe.Add(c.base, uintptr(si)*p.esz)
		return pack(p.index, si), v, true
	}

	return 0, nil, false
}

func (a *Arena) newSlow(p *typePool) (Reference, unsafe.Pointer, bool) {
	// next chunk
	p.index++

	// can use pre-allocated chunk?
	if int(p.index) < len(p.chunks) {
		c := p.chunks[p.index]
		p.current = c
		c.slots[0].rc = 1 // reset ref count to 1
		c.next = 1
		return pack(p.index, 0), c.base, true
	}

	// can allocate new chunk?
	if p.index < maxChunks {
		c := &typeChunk{base: p.alloc(), next: 1}
		p.current = c
		c.slots[0].rc = 1 // reset ref count to 1
		p.chunks = append(p.chunks, c)
		return pack(p.index, 0), c.base, true
	}

	return 0, nil, false
}

// Pin marks the reference as pinned, meaning it will not be released until arena reset.
func (a *Arena) Pin(pool Type, r Reference) {
	p := &a.pools[pool]
	ci, si := unpack(r)
	p.chunks[ci].slots[si].rc = 0
}

// Retain increments the reference count of the resource. If the resource is pinned, it does nothing since pinned
// resources are not reference-counted. If the reference count reaches the maximum value it pins the resource to prevent
// overflow and potential bugs.
func (a *Arena) Retain(pool Type, r Reference) {
	p := &a.pools[pool]
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

// Release decrements the reference count of the resource. If the resource is pinned, it does nothing since pinned
// resources are not reference-counted. If the reference count reaches zero, it adds the resource to the free-list for
// future reuse.
func (a *Arena) Release(pool Type, r Reference) {
	p := &a.pools[pool]
	ci, si := unpack(r)
	s := &p.chunks[ci].slots[si]
	if s.rc > 0 {
		// ref count only if resource is not pinned
		s.rc--
		if s.rc == 0 {
			// add to free-list if ref count reached zero
			s.nextFree = p.free
			if a.zeroOnRelease {
				ptr := unsafe.Add(p.chunks[ci].base, uintptr(si)*p.esz)
				p.reset(ptr)
			}
			p.free = r
		}
	}
}

// Resolve returns a pointer to the value associated with the reference. The returned pointer is temporary and should
// not be stored anywhere. The caller should use Resolve only to access the value of the resource, but not to manage its
// lifetime.
func (a *Arena) Resolve(pool Type, r Reference) unsafe.Pointer {
	p := &a.pools[pool]
	ci, si := unpack(r)
	return unsafe.Add(p.chunks[ci].base, uintptr(si)*p.esz)
}

// Reset clears all allocated values and makes the pool ready for the next cycle.
// If full == true, it drops all resources allocated after pool creation, leaving only the base pre-allocated resources.
// If full == false, it keeps all currently allocated resources for reuse.
func (a *Arena) Reset(pool Type, full bool) {
	p := &a.pools[pool]

	if full {
		// reset tail pointers to nil to release resources allocated after pool creation
		for i := p.base; i < uint32(len(p.chunks)); i++ {
			p.chunks[i] = nil
		}
		p.chunks = p.chunks[:p.base]     // drop all chunks allocated after pool creation (if any)
		p.index = min(p.index, p.base-1) // adjust index so Reset can safely clear all remaining chunks
		p.current = p.chunks[p.index]
	}

	// set values to zero to release potentially held resources when enabled
	for ci := uint32(0); ci <= p.index; ci++ {
		c := p.chunks[ci]
		if a.zeroOnReset {
			ptr := c.base
			for i := uint32(0); i < c.next; i++ {
				if i > 0 {
					ptr = unsafe.Add(ptr, p.esz)
				}
				p.reset(ptr)
			}
		}
		c.next = 0 // reset chunk to initial state
	}

	// reset pool state
	p.index = 0
	p.current = p.chunks[0]
	p.free = 0
}
