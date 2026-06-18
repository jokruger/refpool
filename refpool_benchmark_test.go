package refpool_test

import (
	"testing"

	"github.com/jokruger/refpool"
)

type Value struct {
	a int
	b int
	c int
	d int
}

const sz = 10000
const pa = 100

var (
	ptrs [sz]*Value
	refs [sz]refpool.Reference
	ints [sz]int
)

func BenchmarkAllocateNewValues(b *testing.B) {
	b.Run("Heap", func(b *testing.B) {
		b.ReportAllocs()
		for i := range b.N {
			j := i % sz
			ptrs[j] = &Value{a: i, b: i + 1, c: i + 2, d: i + 3}
		}
	})

	b.Run("Pool", func(b *testing.B) {
		for i := range sz {
			refs[i] = 0
		}
		p := refpool.NewPool[Value](pa, false, false)
		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			r, v, _ := p.New()
			*v = Value{a: i, b: i + 1, c: i + 2, d: i + 3}
			j := i % sz
			if refs[j] != 0 {
				p.Release(refs[j])
			}
			refs[j] = r
		}
	})

	b.Run("Arena", func(b *testing.B) {
		for i := range sz {
			refs[i] = 0
		}
		a := refpool.NewArena(false, false, refpool.With[Value](0, pa))
		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			r, v, _ := a.New(0)
			*(*Value)(v) = Value{a: i, b: i + 1, c: i + 2, d: i + 3}
			j := i % sz
			if refs[j] != 0 {
				a.Release(0, refs[j])
			}
			refs[j] = r
		}
	})
}

func BenchmarkAllocateAfterReset(b *testing.B) {
	const batch = 256 * 4

	b.Run("Pool", func(b *testing.B) {
		p := refpool.NewPool[Value](batch, false, false)
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; {
			p.Reset(false)
			for j := 0; j < batch && i < b.N; j++ {
				i++
				_, v, ok := p.New()
				if !ok {
					b.Fatal("pool New returned ok=false")
				}
				v.a = i
				v.b = i + 1
			}
		}
	})

	b.Run("Arena", func(b *testing.B) {
		a := refpool.NewArena(false, false, refpool.With[Value](0, batch))
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; {
			a.Reset(0, false)
			for j := 0; j < batch && i < b.N; j++ {
				i++
				_, v, ok := a.New(0)
				if !ok {
					b.Fatal("arena New returned ok=false")
				}
				vv := (*Value)(v)
				vv.a = i
				vv.b = i + 1
			}
		}
	})
}

func BenchmarkAccessValues(b *testing.B) {
	b.Run("Pointer", func(b *testing.B) {
		for i := range sz {
			ptrs[i] = &Value{a: i, b: i + 1, c: i + 2, d: i + 3}
		}
		b.ReportAllocs()
		b.ResetTimer()

		var total int
		for i := range b.N {
			v := ptrs[i%sz]
			total += v.a
			v.b++
		}
		ints[0] = total
	})

	b.Run("PoolResolve", func(b *testing.B) {
		p := refpool.NewPool[Value](pa, true, true)
		for i := range sz {
			r, v, _ := p.New()
			*v = Value{a: i, b: i + 1, c: i + 2, d: i + 3}
			refs[i] = r
		}
		b.ReportAllocs()
		b.ResetTimer()

		var total int
		for i := range b.N {
			r := refs[i%sz]
			v := p.Resolve(r)
			total += v.a
			v.b++
		}
		ints[0] = total
	})

	b.Run("ArenaResolve", func(b *testing.B) {
		a := refpool.NewArena(true, true, refpool.With[Value](0, pa))
		for i := range sz {
			r, v, _ := a.New(0)
			*(*Value)(v) = Value{a: i, b: i + 1, c: i + 2, d: i + 3}
			refs[i] = r
		}
		b.ReportAllocs()
		b.ResetTimer()

		var total int
		for i := range b.N {
			r := refs[i%sz]
			v := (*Value)(a.Resolve(0, r))
			total += v.a
			v.b++
		}
		ints[0] = total
	})
}

func BenchmarkRetainValues(b *testing.B) {
	b.Run("Pool", func(b *testing.B) {
		p := refpool.NewPool[Value](pa, true, true)
		for i := range sz {
			r, v, _ := p.New()
			*v = Value{a: i, b: i + 1, c: i + 2, d: i + 3}
			refs[i] = r
		}
		b.ReportAllocs()
		b.ResetTimer()

		for i := range b.N {
			r := refs[i%sz]
			p.Retain(r)
			p.Release(r)
		}
	})

	b.Run("Arena", func(b *testing.B) {
		a := refpool.NewArena(true, true, refpool.With[Value](0, pa))
		for i := range sz {
			r, v, _ := a.New(0)
			*(*Value)(v) = Value{a: i, b: i + 1, c: i + 2, d: i + 3}
			refs[i] = r
		}
		b.ReportAllocs()
		b.ResetTimer()

		for i := range b.N {
			r := refs[i%sz]
			a.Retain(0, r)
			a.Release(0, r)
		}
	})
}
