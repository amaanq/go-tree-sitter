package tree_sitter_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

const optionsTestQuery = "(function_declaration name: (identifier) @name)"

// jsFuncs builds n function declarations, so a query over the result yields n matches.
func jsFuncs(n int) []byte {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "function fn%d(a%d, b%d) { return a%d + b%d; }\n", i, i, i, i, i)
	}
	return []byte(b.String())
}

func optionsTestFixture(t *testing.T, n int) (*tree_sitter.Tree, *tree_sitter.Query, []byte) {
	t.Helper()

	language := tree_sitter.NewLanguage(tree_sitter_javascript.Language())

	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	require.NoError(t, parser.SetLanguage(language))

	source := jsFuncs(n)
	tree := parser.Parse(source, nil)
	require.NotNil(t, tree)
	t.Cleanup(tree.Close)

	// NewQuery returns a concrete *QueryError, so comparing it against a nil error interface
	// would always be non-nil. Check the pointer.
	query, queryErr := tree_sitter.NewQuery(language, optionsTestQuery)
	require.Nil(t, queryErr)
	t.Cleanup(query.Close)

	return tree, query, source
}

// TestMatchesWithOptionsSurvivesSpanReuse pins the lifetime of the TSQueryCursorOptions struct
// passed to ts_query_cursor_exec_with_options.
//
// The C cursor stores that pointer and dereferences it on every advance to reach
// progress_callback. If the struct is allocated in Go, its only reference dies when
// MatchesWithOptions returns, and C is left reading memory the collector may reclaim.
//
// A bare runtime.GC() does not catch that: Go's collector does not move objects, so reclaimed
// memory keeps its old contents until something else reuses the span. This test therefore
// collects and *then* churns allocations in the same size class (the struct is two pointers) to
// overwrite it. Against a Go-allocated struct this reliably corrupts progress_callback and the
// next advance jumps through it — a SIGSEGV whose faulting address equals the program counter.
func TestMatchesWithOptionsSurvivesSpanReuse(t *testing.T) {
	tree, query, source := optionsTestFixture(t, 3)

	// Same size class as TSQueryCursorOptions: two pointer-width fields.
	type twoWords struct {
		a uintptr
		b uintptr
	}

	for round := range 200 {
		cursor := tree_sitter.NewQueryCursor()

		matches := cursor.MatchesWithOptions(query, tree.RootNode(), source, tree_sitter.QueryCursorOptions{
			ProgressCallback: func(tree_sitter.QueryCursorState) bool { return false },
		})

		runtime.GC()
		junk := make([]*twoWords, 0, 4096)
		for i := range 4096 {
			junk = append(junk, &twoWords{a: uintptr(i) | 0x7f7f7f7f7f7f7f7f, b: 0xdeadbeefdeadbeef})
		}
		runtime.KeepAlive(junk)
		runtime.GC()

		count := 0
		for match := matches.Next(); match != nil; match = matches.Next() {
			count++
		}
		require.Equal(t, 3, count, "round %d", round)

		cursor.Close()
	}
}

// TestMatchesWithOptionsCancels checks the callback can actually stop a scan, which is the point
// of this path now that ts_query_cursor_set_timeout_micros is deprecated (removal in 0.26).
func TestMatchesWithOptionsCancels(t *testing.T) {
	tree, query, source := optionsTestFixture(t, 2000)

	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()

	matches := cursor.MatchesWithOptions(query, tree.RootNode(), source, tree_sitter.QueryCursorOptions{
		ProgressCallback: func(tree_sitter.QueryCursorState) bool { return true },
	})

	count := 0
	for match := matches.Next(); match != nil; match = matches.Next() {
		count++
	}

	assert.Less(t, count, 2000, "a callback returning true should have halted the scan early")
}

// TestMatchesWithOptionsInvokesCallback guards the plumbing itself: a callback that never fires
// would make the cancellation test above vacuous.
func TestMatchesWithOptionsInvokesCallback(t *testing.T) {
	tree, query, source := optionsTestFixture(t, 2000)

	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()

	calls := 0
	matches := cursor.MatchesWithOptions(query, tree.RootNode(), source, tree_sitter.QueryCursorOptions{
		ProgressCallback: func(state tree_sitter.QueryCursorState) bool {
			calls++
			return false
		},
	})

	count := 0
	for match := matches.Next(); match != nil; match = matches.Next() {
		count++
	}

	assert.Equal(t, 2000, count, "every function declaration should match")
	assert.Positive(t, calls, "the progress callback should have been invoked")
}

// TestQueryCursorReuseAcrossOptionInstalls exercises the release path. Repeatedly installing new
// options on one cursor, interleaved with the option-free exec (which clears the cursor's options
// in C), must neither leak the previous install nor free something C still reads.
func TestQueryCursorReuseAcrossOptionInstalls(t *testing.T) {
	tree, query, source := optionsTestFixture(t, 200)

	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()

	for i := range 50 {
		var matches tree_sitter.QueryMatches
		if i%3 == 0 {
			matches = cursor.Matches(query, tree.RootNode(), source)
		} else {
			matches = cursor.MatchesWithOptions(query, tree.RootNode(), source, tree_sitter.QueryCursorOptions{
				ProgressCallback: func(tree_sitter.QueryCursorState) bool { return false },
			})
		}

		count := 0
		for match := matches.Next(); match != nil; match = matches.Next() {
			count++
		}
		require.Equal(t, 200, count, "iteration %d", i)

		runtime.GC()
	}
}
