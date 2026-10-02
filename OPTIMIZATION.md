# Read-only memory optimization

`OpenDBWithReader` detects `ReadOnlyMemoryReader` once when opening a database:

```go
type ReadOnlyMemoryReader interface {
	DBReader
	ReadOnlyBytes() []byte
}
```

Implement this capability only when the complete database is stored in immutable,
Go-managed memory. Return the same bytes used by `ReadAt`; returning `nil` opts out.
The library borrows row and string bytes instead of allocating and copying read
buffers. Existing file readers and other `DBReader` implementations keep the
original `ReadAt` path without changes. Short or out-of-range views also fall back
to that path, preserving its read errors and partial-string behavior.

Never modify or reuse the exposed bytes. `Close` may release the reader's own
references, but must not invalidate the storage: returned strings retain it until
they become unreachable. A small retained string can therefore keep an entire old
database alive after replacement. Mmap and reusable buffer pools are not supported
by this capability. Queries may run concurrently against the immutable snapshot;
this does not add concurrent open/reload synchronization to existing callers.

Regression tests use a small generated DB9 fixture, so no downloaded BIN is needed:

```shell
go test -race ./...
```
