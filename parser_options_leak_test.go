package tree_sitter_test

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	. "github.com/tree-sitter/go-tree-sitter"
)

// TestParseWithOptionsReleasesOptionsHandle is a regression test for the
// options-handle leak: ParseWithOptions saved its *ParseOptions into
// mattn/go-pointer's process-global handle map without a matching Unref, which
// pinned the options value (and the ProgressCallback it holds) for the lifetime
// of the process. Releasing the handle after the parse makes a *ParseOptions
// collectable again once the call returns.
//
// The test runs two loops that are identical except for whether the options are
// passed, and counts how many of the created *ParseOptions the GC reclaims via
// finalizers. Loop A passes nil (a control proving finalizers run at all); loop
// B passes the options and must be reclaimed too once the handle is released. No
// grammar is needed: the handle is saved before parsing begins.
func TestParseWithOptionsReleasesOptionsHandle(t *testing.T) {
	const iterations = 10_000

	var finalized atomic.Int64
	onFinalize := func(*ParseOptions) { finalized.Add(1) }
	readEmpty := func(int, Point) []byte { return []byte{} }
	progressNoop := func(ParseState) bool { return false }

	run := func(passOptions bool) int64 {
		finalized.Store(0)
		for i := 0; i < iterations; i++ {
			opts := &ParseOptions{ProgressCallback: progressNoop}
			runtime.SetFinalizer(opts, onFinalize)

			p := NewParser()
			if passOptions {
				p.ParseWithOptions(readEmpty, nil, opts)
			} else {
				p.ParseWithOptions(readEmpty, nil, nil)
			}
			p.Close()
		}
		// Force the GC until the reclaimed count stops changing.
		prev := int64(-1)
		for {
			runtime.GC()
			time.Sleep(20 * time.Millisecond)
			cur := finalized.Load()
			if cur == prev {
				return cur
			}
			prev = cur
		}
	}

	reclaimedNil := run(false)
	reclaimedOpts := run(true)

	t.Logf("nil options:  reclaimed %d / %d", reclaimedNil, iterations)
	t.Logf("with options: reclaimed %d / %d", reclaimedOpts, iterations)

	// Control: if the unreferenced nil-options *ParseOptions are not finalized,
	// the harness is broken and the leak result would be meaningless.
	assert.Greaterf(t, reclaimedNil, int64(iterations/2),
		"control failed: only %d/%d nil-options *ParseOptions were finalized; the test harness is not working",
		reclaimedNil, iterations)

	// Regression: options handed to ParseWithOptions must remain collectable.
	assert.Greaterf(t, reclaimedOpts, int64(iterations/2),
		"options-handle leak: only %d/%d *ParseOptions passed to ParseWithOptions were reclaimed; the binding saved each options handle without a matching Unref",
		reclaimedOpts, iterations)
}
