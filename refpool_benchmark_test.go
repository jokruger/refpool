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

	b.Run("Refpool", func(b *testing.B) {
		for i := range sz {
			refs[i] = 0
		}
		p := refpool.New[Value](pa, nil)
		p.SetZeroOnRelease(false)
		p.SetZeroOnReset(false)
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

	b.Run("RefpoolResolve", func(b *testing.B) {
		p := refpool.New[Value](pa, nil)
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
}
