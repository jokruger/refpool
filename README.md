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

`Pool` is intentionally not concurrency-safe. It does not use atomics or locks
around allocation, reference counting, free-list updates, or resets.

This keeps the hot path small for single-threaded runtimes, arena-like phases,
and systems that already have ownership or scheduling guarantees. If a pool is
shared across goroutines, the caller must provide external synchronization.

## Usage Rules

`Reference(0)` is reserved as an invalid / nil reference. References returned by
`New` are compact integer handles and may be stored or copied, but the pointer
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
old references must not be used until their slots are allocated again by `New`.

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
  pool := refpool.New[Object](1024)

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
