package refpool

// Reference is a handle to a value in the pool.
// 0 corresponds to invalid / nil reference.
type Reference uint32

// Pool of reference-counted values.
type Pool[T any] struct {
	chunks []*chunk[T] // allocated chunks
	next   uint32      // next unused chunk
	free   Reference   // free-list head
}

// Slot index occupies high bits of a reference. Chunk index occupies low bits of a reference and is encoded as
// actual value + 1 which makes Reference = 0 invalid (not pointing to any slot).
const siBits = 8                            // number of bits for slot index (must be less than 16)
const siMask = uint16((1 << siBits) - 1)    // mask for slot index
const ciBits = 32 - siBits                  // number of bits for chunk index
const ciMask = Reference((1 << ciBits) - 1) // mask for chunk index
const chunkSize = 1 << siBits               // number of slots per chunk

// Chunk of slots for storing values.
type chunk[T any] struct {
	slots [chunkSize]slot[T] // fixed-size array of slots
	next  uint16             // next never used slot index
}

// Single value slot.
type slot[T any] struct {
	rc    uint32 // reference count (0 = slot is not used yet, or it is used but should not be reference counted)
	next  uint32 // next unused slot index
	value T      // value stored in the slot
}

// Assemble a reference from chunk index and slot index.
func pack(ci uint32, si uint16) Reference {
	return Reference(si&siMask)<<ciBits | Reference(ci+1)
}

// Disassemble a reference into chunk index and slot index. Reference must be valid (not zero).
func unpack(r Reference) (uint32, uint16) {
	ci := uint32(r&ciMask) - 1
	si := uint16(r>>ciBits) & siMask
	return ci, si
}
