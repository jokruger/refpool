# refpool

Is a Go package for allocating, retaining, releasing, and reusing values through
compact integer handles.

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

## Install

Run `go get github.com/jokruger/refpool`

## License

This project is licensed under the MIT License. See the `LICENSE` file for details.
