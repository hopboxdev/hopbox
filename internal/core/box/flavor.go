package box

// Flavor is a box's resource allocation — what krillbox shows as "2 vCPU ·
// 1024 MiB". CPUMillis is milli-cores (1000 = 1 vCPU); MemMB is mebibytes. A
// zero field means the provider default (unbounded).
type Flavor struct {
	CPUMillis int64
	MemMB     int64
}

// builtinFlavors is the starter catalog. It is intentionally small; a real
// deployment will drive named flavors from config (and microVM/GPU shapes).
var builtinFlavors = map[string]Flavor{
	"small":  {CPUMillis: 1000, MemMB: 1024},
	"medium": {CPUMillis: 2000, MemMB: 2048},
	"large":  {CPUMillis: 4000, MemMB: 4096},
}

// ResolveFlavor returns the named built-in flavor, or false if the name is
// unknown (callers decide whether to fall back to a default or reject).
func ResolveFlavor(name string) (Flavor, bool) {
	f, ok := builtinFlavors[name]
	return f, ok
}

// Apply sets the box's resource caps from f. Zero fields leave the provider
// default in place (so a flavor can cap CPU without forcing a memory cap).
func (b *Box) Apply(f Flavor) {
	if f.CPUMillis > 0 {
		b.CPUMillis = f.CPUMillis
	}
	if f.MemMB > 0 {
		b.MemMB = f.MemMB
	}
}
