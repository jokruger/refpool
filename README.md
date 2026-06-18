# refpool

refpool is a Go package for allocating, retaining, releasing, and reusing values
through compact integer handles.

It is designed for systems that need stable resource identifiers without
exposing raw pointers, such as virtual machines, interpreters, scripting
runtimes, entity stores, and tagged/boxed value representations. Values are
stored internally in segmented arrays, allowing handles to be resolved to
pointers in constant time while avoiding large contiguous allocations.

Unlike `sync.Pool`, refpool gives each allocated value an integer handle that
can be stored, copied, tagged, or embedded inside a VM value. Unlike a simple
arena, values can be individually released through reference counting and later
reused through a free-list. A global reset operation is also provided for
arena-style lifetime management when all values can be discarded together.

The package is intended to reduce GC pressure, minimize heap allocations, and
make resource ownership explicit in performance-sensitive Go programs.

## Concurrency

`Pool` and `Arena` are intentionally not concurrency-safe. They do not use atomics or locks
around allocation, reference counting, free-list updates, or resets.

This keeps the hot path small for single-threaded runtimes, arena-like phases,
and systems that already have ownership or scheduling guarantees. If a pool is
shared across goroutines, the caller must provide external synchronization.

## Pool

`Pool[T]` is a single-type, typed generic pool. Create one with `NewPool[T]` and interact
with it through methods on the returned pointer.

## Arena

`Arena` is a multi-type pool that hosts up to 256 independent typed sub-pools, each
identified by a `Type` (a `uint8`). This lets a single arena manage all resource
types for a subsystem — such as a virtual machine or compiler pass — and reset them
all in bulk.

### Creating an Arena

```go
arena := refpool.NewArena(
    refpool.With[MyStruct](myType, 1024),   // pre-allocate 1024 slots of type MyStruct
    refpool.With[string](strType, 512),     // pre-allocate 512 string slots
    refpool.WithZeroOnRelease(false),       // optional: skip zeroing on Release
)
```

Each `With[T](pool, preAlloc)` option registers a pool for the given `Type` identifier and
pre-allocates at least `preAlloc` slots. `WithZeroOnRelease` applies to all sub-pools.

### Arena Usage Rules

`Reference(0)` is reserved as the invalid/nil reference. The same rules that apply to
`Pool` apply to each sub-pool within an `Arena`:

- Call `Retain` whenever a logical copy of a reference is created.
- Each `Retain` must be matched by a `Release`, unless the slot is pinned.
- `Pin` marks a slot as arena-owned; `Retain`/`Release` have no effect on pinned slots.
- `Resolve` returns a temporary pointer — do not store it.
- After `Reset`, old references must not be used until their slots are re-allocated.

### Arena API

| Function / Method | Description |
|---|---|
| `NewArena(opts...)` | Creates a new Arena with the given options. |
| `With[T](pool, preAlloc)` | Registers type `T` for `pool` and pre-allocates slots. |
| `WithZeroOnRelease(flag)` | Controls whether `Release` zeroes the value (default `true`). |
| `arena.New(pool)` | Allocates (or reuses) a slot; returns `(Reference, any, bool)`. |
| `arena.Resolve(pool, r)` | Returns `any` pointer for the reference. |
| `Resolve[T](arena, pool, r)` | Generic helper; returns a typed `*T`. |
| `arena.Retain(pool, r)` | Increments reference count. |
| `arena.Release(pool, r)` | Decrements reference count; frees slot at zero. |
| `arena.Pin(pool, r)` | Pins a slot; bypasses reference counting. |
| `arena.Reset(pool)` | Drops extra chunks and resets the free-list for `pool`. |
| `arena.Stats(pool)` | Returns `(allocated, free int)` for `pool`. |

### Arena Example

```go
package main

import (
  "fmt"

  "github.com/jokruger/refpool"
)

const (
  TypeNode refpool.Type = 0
  TypeEdge refpool.Type = 1
)

type Node struct{ Label string }
type Edge struct{ From, To refpool.Reference }

func main() {
  arena := refpool.NewArena(
    refpool.With[Node](TypeNode, 1024),
    refpool.With[Edge](TypeEdge, 2048),
  )

  nRef, nAny, ok := arena.New(TypeNode)
  if !ok {
    panic("arena overflow")
  }
  nAny.(*Node).Label = "root"

  eRef, eAny, ok := arena.New(TypeEdge)
  if !ok {
    panic("arena overflow")
  }
  eAny.(*Edge).From = nRef

  fmt.Println(refpool.Resolve[Node](arena, TypeNode, nRef).Label) // root
  fmt.Println(refpool.Resolve[Edge](arena, TypeEdge, eRef).From == nRef) // true

  arena.Retain(TypeNode, nRef)
  arena.Release(TypeNode, nRef)
  arena.Release(TypeNode, nRef) // final release — slot returned to free-list

  // Bulk reset for next cycle.
  arena.Reset(TypeNode)
  arena.Reset(TypeEdge)
}
```



## Pool Usage Rules

`Reference(0)` is reserved as an invalid / nil reference. References returned by
`NewPool` are compact integer handles and may be stored or copied, but the pointer
returned by `Resolve` is temporary and must not be retained.

Use `Retain` whenever a logical copy of a reference is created and may outlive
the original owner. Each retained reference must eventually be matched by a
`Release`, unless the value is pinned.

`Release` decrements the reference count. When the count reaches zero, the value
is reset to the zero value of `T` and the slot is added to the free-list for
reuse. This zeroing is enabled by default and can be disabled per pool with
`SetZeroOnRelease(false)` for throughput-focused workloads.

`Pin` marks a value as pool-owned until the next reset. Pinned values are not
reference-counted, so `Retain` and `Release` have no effect on them. Values are
also pinned automatically if the reference count reaches `math.MaxUint32`.

`Reset` clears all currently allocated values and keeps all chunks for reuse.
Reset zeroing is enabled by default and can be disabled per pool with
`SetZeroOnReset(false)` for throughput-focused workloads that can tolerate value
retention between reset cycles.
`ResetFull` also drops chunks allocated after pool creation. After either reset,
old references must not be used until their slots are allocated again by `NewPool`.

## Example

```go
package main

import (
  "fmt"

  "github.com/jokruger/refpool"
)

type Object struct {
  Name string
}

func main() {
  pool := refpool.NewPool[Object](1024)

  ref, obj, ok := pool.New()
  if !ok {
    panic("refpool overflow")
  }
  obj.Name = "alpha"

  // Logical copy: retain once per extra owner.
  pool.Retain(ref)

  // Resolve by handle when needed.
  fmt.Println(pool.Resolve(ref).Name) // alpha

  // Release all owners.
  pool.Release(ref)
  pool.Release(ref)

  // Slot can now be reused.
  reusedRef, reusedObj, ok := pool.New()
  if !ok {
    panic("refpool overflow")
  }
  reusedObj.Name = "beta"
  fmt.Println(reusedRef == ref) // often true (free-list reuse)
}
```

## Install

Run `go get github.com/jokruger/refpool`

## License

This project is licensed under the MIT License. See the `LICENSE` file for details.
