package refpool

import "testing"

type benchmarkValue struct {
	a int
	b int
	c int
	d int
}

var benchmarkPointerSink [3]*benchmarkValue
var benchmarkReferenceSink [3]Reference
var benchmarkIntSink int

func BenchmarkAllocateNewValues(b *testing.B) {
	b.Run("Heap", func(b *testing.B) {
		b.ReportAllocs()

		for i := range b.N {
			v := &benchmarkValue{a: i, b: i + 1, c: i + 2, d: i + 3}
			benchmarkPointerSink[0] = v
		}
	})

	b.Run("Refpool", func(b *testing.B) {
		p := New[benchmarkValue](b.N)

		b.ReportAllocs()
		b.ResetTimer()

		for i := range b.N {
			r, _ := p.New(&benchmarkValue{a: i, b: i + 1, c: i + 2, d: i + 3})
			benchmarkReferenceSink[0] = r
		}
	})
}

func BenchmarkAccessValues(b *testing.B) {
	b.Run("Pointer", func(b *testing.B) {
		v1 := &benchmarkValue{a: 1, b: 2, c: 3, d: 4}
		v2 := &benchmarkValue{a: 1, b: 2, c: 3, d: 4}
		v3 := &benchmarkValue{a: 1, b: 2, c: 3, d: 4}

		b.ReportAllocs()
		b.ResetTimer()

		var total int
		for range b.N {
			total += v1.a
			v1.b++
			total += v2.a
			v2.b++
			total += v3.a
			v3.b++
		}
		benchmarkIntSink = total
		benchmarkPointerSink[0] = v1
		benchmarkPointerSink[1] = v2
		benchmarkPointerSink[2] = v3
	})

	b.Run("RefpoolResolve", func(b *testing.B) {
		p := New[benchmarkValue](1)
		r1, _ := p.New(&benchmarkValue{a: 1, b: 2, c: 3, d: 4})
		r2, _ := p.New(&benchmarkValue{a: 1, b: 2, c: 3, d: 4})
		r3, _ := p.New(&benchmarkValue{a: 1, b: 2, c: 3, d: 4})

		b.ReportAllocs()
		b.ResetTimer()

		var total int
		for range b.N {
			v := p.Resolve(r1)
			total += v.a
			v.b++
			v = p.Resolve(r2)
			total += v.a
			v.b++
			v = p.Resolve(r3)
			total += v.a
			v.b++
		}
		benchmarkIntSink = total
		benchmarkReferenceSink[0] = r1
		benchmarkReferenceSink[1] = r2
		benchmarkReferenceSink[2] = r3
	})
}
