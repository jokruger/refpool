package refpool

import (
	"math"
	"unsafe"
)

type Arena struct {
	pools         [256]typePool
	zeroOnRelease bool // whether Release should reset value to zero
}

type typePool struct {
	alloc  func() unsafe.Pointer    // allocate typed buffer and return base pointer
	reset  func(ptr unsafe.Pointer) // reset a value to zero
	elemSz uintptr                  // element size for pointer arithmetic

	chunks []*typeChunk // allocated chunks
	free   Reference    // free-list head
	index  uint32       // current chunk index for linear bump allocation
	base   uint32       // number of pre-allocated chunks calculated when new pool was created
}

type typeChunk struct {
	base  unsafe.Pointer      // base pointer to first value in typed buffer
	next  uint32              // next uninitialized slot for linear bump allocation
	slots [chunkSize]typeSlot // fixed-size array of slots
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
	*(*T)(ptr) = *new(T)
}

func NewArena(opts ...func(*Arena)) *Arena {
	a := &Arena{zeroOnRelease: true}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func WithZeroOnRelease(flag bool) func(*Arena) {
	return func(a *Arena) {
		a.zeroOnRelease = flag
	}
}

func With[T any](pool Type, preAlloc int) func(*Arena) {
	var zero T

	return func(a *Arena) {
		p := &a.pools[pool]
		p.alloc = allocBuffer[T]
		p.reset = resetPtr[T]
		p.elemSz = unsafe.Sizeof(zero)

		// calc how many chunks we need to pre-allocate
		cs := preAlloc / chunkSize
		if preAlloc%chunkSize != 0 {
			cs++
		}
		cs = max(1, cs)

		// init pool
		p.base = uint32(cs)
		p.chunks = make([]*typeChunk, 0, max(cs, minChunks))
		for range cs {
			a.grow(p)
		}
	}
}

// Stats returns the current pool stats.
func (a *Arena) Stats(pool Type) (allocated, free int) {
	p := &a.pools[pool]

	allocated = len(p.chunks) * chunkSize

	// Free-list slots are reusable slots returned by Release.
	free = 0

	i := p.free
	for i != 0 {
		free++
		ci, si := unpack(i)
		i = p.chunks[ci].slots[si].nextFree
	}

	// Add still-uninitialized slots in each chunk managed by linear bump allocation.
	for _, c := range p.chunks {
		free += int(chunkSize - c.next)
	}

	return allocated, free
}

// New creates a new (or re-use free) reference to a value in the pool. The value is not initialized and should be set
// by the caller. Returns the reference, value pointer and flag indicating whether allocation was successful.
func (a *Arena) New(pool Type) (Reference, unsafe.Pointer, bool) {
	p := &a.pools[pool]

	// Reuse a released slot first.
	if p.free != 0 {
		r := p.free
		ci, si := unpack(r)
		c := p.chunks[ci]
		s := &c.slots[si]
		p.free = s.nextFree
		s.rc = 1 // reset ref count to 1
		v := unsafe.Add(c.base, uintptr(si)*p.elemSz)
		return r, v, true
	}

	// Fast path: linear bump allocation in current chunk.
	for {
		c := p.chunks[p.index]
		if c.next < chunkSize {
			si := c.next
			c.next++
			c.slots[si].rc = 1 // reset ref count to 1
			v := unsafe.Add(c.base, uintptr(si)*p.elemSz)
			return pack(p.index, si), v, true
		}

		// Move to next pre-allocated chunk if available.
		if int(p.index)+1 < len(p.chunks) {
			p.index++
			continue
		}

		// Need to grow by one chunk.
		if len(p.chunks) >= maxChunks {
			return 0, nil, false
		}
		a.grow(p)
		p.index = uint32(len(p.chunks) - 1)
	}
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
				ptr := unsafe.Add(p.chunks[ci].base, uintptr(si)*p.elemSz)
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
	return unsafe.Add(p.chunks[ci].base, uintptr(si)*p.elemSz)
}

// Reset clears all allocated values and makes the pool ready for the next cycle.
func (a *Arena) Reset(pool Type) {
	p := &a.pools[pool]
	a.reset(p)
}

func (a *Arena) grow(p *typePool) {
	// allocate new chunk
	base := p.alloc()
	c := &typeChunk{base: base}

	// add new chunk to pool
	p.chunks = append(p.chunks, c)
}

func (a *Arena) reset(p *typePool) {
	// release unused chunks
	for i := p.base; i < uint32(len(p.chunks)); i++ {
		p.chunks[i] = nil
	}
	p.chunks = p.chunks[:p.base]

	// reset bump cursors in retained chunks
	for ci := range p.chunks {
		p.chunks[ci].next = 0
	}
	p.free = 0
	p.index = 0
}
