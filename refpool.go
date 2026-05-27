package refpool

type slot[T any] struct {
	rc    uint32 // reference count (0 = slot is not used yet, or it is used but should not be reference counted)
	next  uint32 // next free slot index
	value T      // value stored in the slot
}

type chunk[T any] [1024]slot[T] // fixed-size array of slots

type Pool[T any] struct {
	next   uint32      // never used element index
	free   uint32      // free-list head
	chunks []*chunk[T] // allocated chunks
}
