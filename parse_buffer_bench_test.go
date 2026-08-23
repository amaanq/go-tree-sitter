package tree_sitter_test

import (
	"strings"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// benchSource is a synthetic Go file of roughly the size a real source file reaches, built
// once so both benchmarks parse byte-identical input.
//
// Size matters here: Parse's callback returns the entire remaining document on each
// invocation and readUTF8 copies it twice, so the overhead this measures scales with the
// document, not with the number of nodes. A few hundred bytes would hide it.
func benchSource(sizeHint int) []byte {
	const unit = "func f%d(a int, b string) (int, error) { x := a + 1; _ = b; return x, nil }\n"

	var b strings.Builder
	b.Grow(sizeHint + len(unit))
	b.WriteString("package main\n\n")
	for i := 0; b.Len() < sizeHint; i++ {
		b.WriteString(strings.Replace(unit, "%d", string(rune('a'+i%26)), 1))
	}

	return []byte(b.String())
}

func benchmarkParse(b *testing.B, source []byte, useBuffer bool) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		b.Fatalf("setting language: %s", err)
	}

	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		parser.Reset()

		var tree *tree_sitter.Tree
		if useBuffer {
			tree = parser.ParseBuffer(source, nil)
		} else {
			tree = parser.Parse(source, nil)
		}
		if tree == nil {
			b.Fatal("no tree returned")
		}
		tree.Close()
	}
}

func BenchmarkParse(b *testing.B) {
	for _, size := range []int{4 << 10, 64 << 10, 512 << 10} {
		source := benchSource(size)
		b.Run(sizeName(size), func(b *testing.B) { benchmarkParse(b, source, false) })
	}
}

func BenchmarkParseBuffer(b *testing.B) {
	for _, size := range []int{4 << 10, 64 << 10, 512 << 10} {
		source := benchSource(size)
		b.Run(sizeName(size), func(b *testing.B) { benchmarkParse(b, source, true) })
	}
}

func sizeName(size int) string {
	switch {
	case size >= 1<<20:
		return "1MiB"
	case size >= 512<<10:
		return "512KiB"
	case size >= 64<<10:
		return "64KiB"
	default:
		return "4KiB"
	}
}
