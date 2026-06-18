package refpool

import (
	"testing"
)

func TestPackUnpackRoundTrip(t *testing.T) {
	cases := []struct {
		ci uint32
		si uint32
	}{
		{0, 0},
		{0, 255},
		{0, 256},
		{0, 1234567890},
		{0, 4294967295},
		{1, 0},
		{1, 1},
		{42, 7},
		{1234567890, 0},
		{4294967294, 0},
		{4294967294, 4294967295},
		{4294967295 / 2, 4294967295 / 2},
	}

	for _, c := range cases {
		r := pack(c.ci, c.si)
		if r == 0 {
			t.Errorf("pack(%d, %d) produced invalid Reference 0", c.ci, c.si)
		}
		gotCI, gotSI := unpack(r)
		if gotCI != c.ci || gotSI != c.si {
			t.Errorf("round-trip failed for (ci=%d, si=%d): packed=%#x, unpacked=(%d, %d)", c.ci, c.si, r, gotCI, gotSI)
		}
	}
}

func TestPackNeverReturnsZero(t *testing.T) {
	// Every valid (ci, si) pair must pack to a non-zero Reference, because Reference(0) is reserved for "invalid / nil".
	if pack(0, 0) == 0 {
		t.Fatal("pack(0, 0) must not return 0")
	}
}

func TestPackUsesExpectedLayout(t *testing.T) {
	r := pack(0, 1)
	want := Reference(1)<<32 | 1
	if r != want {
		t.Fatalf("pack(0, 1) = %#x, want %#x", r, want)
	}

	ci, si := unpack(r)
	if ci != 0 || si != 1 {
		t.Fatalf("unpack(pack(0, 1)) = (%d, %d), want (0, 1)", ci, si)
	}
}

func TestPackMaxValidValues(t *testing.T) {
	ci := uint32(maxChunks - 1)
	si := uint32(chunkSize - 1)

	r := pack(ci, si)
	if r == 0 {
		t.Fatal("pack(max valid values) must not return 0")
	}

	gotCI, gotSI := unpack(r)
	if gotCI != ci || gotSI != si {
		t.Fatalf("round-trip failed for max valid values: packed=%#x, unpacked=(%d, %d)", r, gotCI, gotSI)
	}
}
