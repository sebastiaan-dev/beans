package beancore

import (
	"os"
	"testing"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/config"
)

// Benchmarks for the read paths affected by copy-on-write. Set
// BEANS_BENCH_DIR to a .beans directory (e.g. a generated store of 8000
// beans) to measure a realistic store; otherwise they are skipped.
func benchCore(b *testing.B) *Core {
	b.Helper()
	dir := os.Getenv("BEANS_BENCH_DIR")
	if dir == "" {
		b.Skip("BEANS_BENCH_DIR not set")
	}
	core := New(dir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		b.Fatal(err)
	}
	return core
}

// BenchmarkAll measures All, which returns the shared snapshots.
func BenchmarkAll(b *testing.B) {
	core := benchCore(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = core.All()
	}
}

// BenchmarkAllCloned measures what All would cost if it returned copies.
func BenchmarkAllCloned(b *testing.B) {
	core := benchCore(b)
	b.ReportAllocs()
	for b.Loop() {
		all := core.All()
		for i, bn := range all {
			all[i] = bn.Clone()
		}
	}
}

// BenchmarkGet measures Get, which returns a copy.
func BenchmarkGet(b *testing.B) {
	core := benchCore(b)
	ids := make([]string, 0, len(core.beans))
	for id := range core.beans {
		ids = append(ids, id)
	}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		_, _ = core.Get(ids[i%len(ids)])
		i++
	}
}

// BenchmarkMutate measures an in-memory (non-persisting) mutation.
func BenchmarkMutate(b *testing.B) {
	core := benchCore(b)
	var id string
	for id = range core.beans {
		break
	}
	statuses := []string{"todo", "in-progress"}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		if _, err := core.Mutate(id, nil, func(_ *Tx, bn *bean.Bean) error {
			bn.Status = statuses[i%2]
			return nil
		}, WithPersist(false)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRelationsAll measures resolving every bean's relations (incoming
// links and blockers), as a list query selecting children/blocking/blockedBy
// and isBlocked does.
func BenchmarkRelationsAll(b *testing.B) {
	core := benchCore(b)
	all := core.All()
	b.ReportAllocs()
	for b.Loop() {
		for _, bn := range all {
			_ = core.FindIncomingLinks(bn.ID)
			_ = core.IsBlocked(bn.ID)
		}
	}
}

// BenchmarkLoad measures loading the store from disk (including building
// the link index).
func BenchmarkLoad(b *testing.B) {
	core := benchCore(b)
	b.ReportAllocs()
	for b.Loop() {
		if err := core.Load(); err != nil {
			b.Fatal(err)
		}
	}
}
