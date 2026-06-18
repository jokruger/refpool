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
type Reference = uint64

// Type is a pool type identifier.
type Type = uint8

// Number of slots per chunk
const chunkSize = 256

// Min size of chunks slice
const minChunks = 32

// Max number of chunks
const maxChunks = math.MaxUint32 - 1

// Assemble a reference from chunk index and slot index.
func pack(ci uint32, si uint32) Reference {
	return Reference(ci+1) | Reference(si)<<32
}

// Disassemble a reference into chunk index and slot index. Reference must be valid (not zero).
func unpack(r Reference) (uint32, uint32) {
	return uint32(r) - 1, uint32(r >> 32)
}
