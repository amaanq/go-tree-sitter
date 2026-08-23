package tree_sitter_test

import (
	"strings"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// ParseBuffer takes a different route into the C parser than Parse (ts_parser_parse_string
// against the caller's bytes, rather than the chunked TSInput callback), so the thing worth
// testing is that the two agree — a faster path that produces a different tree is not a
// faster path.
func TestParseBufferMatchesParse(t *testing.T) {
	sources := map[string]string{
		"empty":       "",
		"trivial":     "package main\n",
		"typical":     "package main\n\nimport \"fmt\"\n\ntype T struct{ A int }\n\nfunc (t T) M() error { fmt.Println(t.A); return nil }\n",
		"with errors": "package main\n\nfunc broken( { }\n",
		// Exercises the multi-chunk case: Parse's callback returns the whole remainder in
		// one go, but a large input is where the two paths would diverge if the callback
		// were ever invoked more than once.
		"large": "package main\n" + strings.Repeat("func f() { _ = 1 + 2 }\n", 20000),
		// Multi-byte runes shift byte offsets away from rune counts, which is exactly what
		// a length-in-bytes API can get wrong.
		"unicode": "package main\n\nvar s = \"日本語のテキスト — em dash\"\nvar t = \"🙂🙃\"\n",
		// A NUL byte mid-buffer terminates a C string; ts_parser_parse_string takes an
		// explicit length, so it must survive one.
		"embedded NUL": "package main\n\nvar s = \"a\x00b\"\n",
	}

	language := tree_sitter.NewLanguage(tree_sitter_go.Language())

	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			viaParse := tree_sitter.NewParser()
			defer viaParse.Close()
			if err := viaParse.SetLanguage(language); err != nil {
				t.Fatalf("setting language: %s", err)
			}

			viaBuffer := tree_sitter.NewParser()
			defer viaBuffer.Close()
			if err := viaBuffer.SetLanguage(language); err != nil {
				t.Fatalf("setting language: %s", err)
			}

			expected := viaParse.Parse([]byte(source), nil)
			if expected == nil {
				t.Fatal("Parse returned no tree")
			}
			defer expected.Close()

			actual := viaBuffer.ParseBuffer([]byte(source), nil)
			if actual == nil {
				t.Fatal("ParseBuffer returned no tree")
			}
			defer actual.Close()

			// S-expressions capture node types, structure, and error nodes, so an
			// equality check here covers the parse result as a whole rather than a
			// hand-picked property of it.
			if want, got := expected.RootNode().ToSexp(), actual.RootNode().ToSexp(); want != got {
				t.Errorf("trees differ\n Parse: %s\nBuffer: %s", want, got)
			}

			// Byte extents are what a caller slices the source with, so a mismatch here
			// would corrupt every downstream range even with an identical shape.
			if want, got := expected.RootNode().EndByte(), actual.RootNode().EndByte(); want != got {
				t.Errorf("end byte: Parse %d, ParseBuffer %d", want, got)
			}
			if want, got := expected.RootNode().EndPosition(), actual.RootNode().EndPosition(); want != got {
				t.Errorf("end position: Parse %+v, ParseBuffer %+v", want, got)
			}
		})
	}
}

// The returned Tree must not depend on the caller's buffer staying alive or unmodified,
// which is the load-bearing claim in ParseBuffer's doc comment: Tree-sitter records offsets
// rather than retaining the text.
func TestParseBufferTreeOutlivesBuffer(t *testing.T) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("setting language: %s", err)
	}

	source := []byte("package main\n\nfunc f() int { return 7 }\n")
	tree := parser.ParseBuffer(source, nil)
	if tree == nil {
		t.Fatal("ParseBuffer returned no tree")
	}
	defer tree.Close()

	before := tree.RootNode().ToSexp()

	// Scribble over the buffer the parse read from. A tree holding a pointer into it would
	// change shape or crash here.
	for i := range source {
		source[i] = 'x'
	}

	if after := tree.RootNode().ToSexp(); before != after {
		t.Errorf("tree changed after the source buffer was overwritten\nbefore: %s\n after: %s", before, after)
	}
}

func TestParseBufferWithoutLanguage(t *testing.T) {
	// Parse returns nil when no language is set; ParseBuffer must not diverge by panicking.
	parser := tree_sitter.NewParser()
	defer parser.Close()

	if tree := parser.ParseBuffer([]byte("package main\n"), nil); tree != nil {
		tree.Close()
		t.Error("expected no tree when no language is set")
	}
}
