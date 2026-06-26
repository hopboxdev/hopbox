package box

import "testing"

func TestResolveFlavor(t *testing.T) {
	f, ok := ResolveFlavor("medium")
	if !ok || f.CPUMillis != 2000 || f.MemMB != 2048 {
		t.Fatalf("medium = %+v ok=%v", f, ok)
	}
	if _, ok := ResolveFlavor("nope"); ok {
		t.Fatal("unknown flavor must report ok=false")
	}
}

func TestApplyFlavor(t *testing.T) {
	b := New("default", "alice", "proj", "img")
	b.Apply(Flavor{CPUMillis: 3000, MemMB: 4096})
	if b.CPUMillis != 3000 || b.MemMB != 4096 {
		t.Fatalf("apply set cpu=%d mem=%d", b.CPUMillis, b.MemMB)
	}
	// Zero fields don't clobber existing caps.
	b.Apply(Flavor{CPUMillis: 0, MemMB: 8192})
	if b.CPUMillis != 3000 || b.MemMB != 8192 {
		t.Fatalf("zero CPU should not clear: cpu=%d mem=%d", b.CPUMillis, b.MemMB)
	}
}
