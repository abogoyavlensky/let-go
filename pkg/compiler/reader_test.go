/*
 * Copyright (c) 2021 Marcin Gasperowicz <xnooga@gmail.com>
 * SPDX-License-Identifier: MIT
 */

package compiler

import (
	"strings"
	"testing"

	"github.com/nooga/let-go/pkg/vm"
	"github.com/stretchr/testify/assert"
)

func TestReaderBasic(t *testing.T) {
	cases := map[string]vm.Value{
		"1":                          vm.Int(1),
		"+1":                         vm.Int(1),
		"-1":                         vm.Int(-1),
		"987654321":                  vm.Int(987654321),
		"+987654321":                 vm.Int(987654321),
		"-987654321":                 vm.Int(-987654321),
		"true":                       vm.TRUE,
		"false":                      vm.FALSE,
		"nil":                        vm.NIL,
		"foo":                        vm.Symbol("foo"),
		"()":                         vm.EmptyList,
		"(    )":                     vm.EmptyList,
		"(1 2)":                      vm.EmptyList.Cons(vm.Int(2)).Cons(vm.Int(1)),
		"\"hello\"":                  vm.String("hello"),
		"\"h\\\"el\\tl\\\\o\"":       vm.String("h\"el\tl\\o"),
		":foo":                       vm.Keyword("foo"),
		"\\F":                        vm.Char('F'),
		"\\newline":                  vm.Char('\n'),
		"\\u1234":                    vm.Char('\u1234'),
		"\\o300":                     vm.Char(rune(0300)),
		"\\u03A9":                    vm.Char('Ω'),
		"[]":                         vm.ArrayVector{},
		"[1 :foo true]":              vm.ArrayVector{vm.Int(1), vm.Keyword("foo"), vm.TRUE},
		"'foo":                       vm.EmptyList.Cons(vm.Symbol("foo")).Cons(vm.Symbol("quote")),
		"^:foo zoo":                  vm.EmptyList.Cons(vm.NewPersistentMap([]vm.Value{vm.Keyword("foo"), vm.TRUE})).Cons(vm.Symbol("zoo")).Cons(vm.Symbol("with-meta")),
		"^:foo ^:bar zoo":            vm.EmptyList.Cons(vm.NewPersistentMap([]vm.Value{vm.Keyword("foo"), vm.TRUE, vm.Keyword("bar"), vm.TRUE})).Cons(vm.Symbol("zoo")).Cons(vm.Symbol("with-meta")),
		"^{:foo 1 :baz 2} ^:bar zoo": vm.EmptyList.Cons(vm.NewPersistentMap([]vm.Value{vm.Keyword("foo"), vm.Int(1), vm.Keyword("baz"), vm.Int(2), vm.Keyword("bar"), vm.TRUE})).Cons(vm.Symbol("zoo")).Cons(vm.Symbol("with-meta")),
		"^:bar ^{:foo 1 :baz 2} zoo": vm.EmptyList.Cons(vm.NewPersistentMap([]vm.Value{vm.Keyword("bar"), vm.TRUE, vm.Keyword("foo"), vm.Int(1), vm.Keyword("baz"), vm.Int(2)})).Cons(vm.Symbol("zoo")).Cons(vm.Symbol("with-meta")),
	}

	for p, e := range cases {
		r := NewLispReader(strings.NewReader(p), "<reader>")
		o, err := r.Read()
		assert.NoError(t, err)
		assert.Equal(t, e, o)
	}
}

func TestSimpleCall(t *testing.T) {
	p := "(+ 40 2)"
	r := NewLispReader(strings.NewReader(p), "<reader>")
	o, err := r.Read()
	assert.NoError(t, err)

	out, err := vm.ListType.Box([]vm.Value{
		vm.Symbol("+"),
		vm.Int(40),
		vm.Int(2),
	})

	assert.NoError(t, err)
	assert.Equal(t, out, o)
}

func TestReaderConditionalSplicing(t *testing.T) {
	cases := map[string]vm.Value{
		"(a #?@(:cljs [nil] :default []) b)": vm.EmptyList.Cons(vm.Symbol("b")).Cons(vm.Symbol("a")),
		"(a #?@(:cljs [] :default [x y]) b)": vm.EmptyList.Cons(vm.Symbol("b")).Cons(vm.Symbol("y")).Cons(vm.Symbol("x")).Cons(vm.Symbol("a")),
	}

	for p, e := range cases {
		r := NewLispReader(strings.NewReader(p), "<reader>")
		o, err := r.Read()
		assert.NoError(t, err)
		assert.Equal(t, e, o)
	}
}

// TestReaderConditionalWhitespaceAfterPrefix asserts that whitespace
// between a prefix reader macro and the form it consumes is preserved
// in the captured snippet, so the re-parsed value's token offsets and
// FormSource positions remain aligned with the original source.
func TestReaderConditionalWhitespaceAfterPrefix(t *testing.T) {
	in := "#?(:lg '\n  :ok :cljs :x)"
	r := NewLispReaderTokenizing(strings.NewReader(in), "<reader>")
	_, err := r.Read()
	assert.NoError(t, err)
	// Find the token covering ":ok" and confirm it points at the actual
	// `:ok` in the input rather than a position shifted by the dropped
	// whitespace.
	foundOk := false
	for _, tok := range r.Tokens {
		if tok.End <= tok.Start || tok.End > len(in) {
			continue
		}
		if in[tok.Start:tok.End] == ":ok" {
			foundOk = true
			break
		}
	}
	assert.True(t, foundOk, "expected token spanning the literal :ok in %q; got tokens %+v", in, r.Tokens)
}

// TestReaderConditionalLeftoverError asserts that when the skipper's
// captured snippet has unconsumed non-whitespace/non-comment content
// after sub-reader Read (e.g. `' ;c\n :ok` parses as `(quote VOID)`
// leaving `:ok` stranded), the conditional surfaces an error instead
// of silently dropping the trailing text.
func TestReaderConditionalLeftoverError(t *testing.T) {
	in := "#?(:lg ' ;c\n :ok)"
	r := NewLispReader(strings.NewReader(in), "<reader>")
	_, err := r.Read()
	assert.Error(t, err, "expected leftover-content error for %q", in)
}

// TestReaderConditionalTokenOrder pins the REPL-highlighter invariant:
// after reading a reader-conditional, r.Tokens must be in monotonically
// non-decreasing Start order, including when the priority-chosen branch
// is followed by another branch in source.
func TestReaderConditionalTokenOrder(t *testing.T) {
	inputs := []string{
		"#?(:lg :ok :cljs :y)",
		"#?(:cljs :y :lg :ok)",
		"#?(:default :first :lg :winner)",
		"#?(:lg :first :default :second)",
	}
	for _, in := range inputs {
		r := NewLispReaderTokenizing(strings.NewReader(in), "<reader>")
		_, err := r.Read()
		assert.NoError(t, err, "input: %s", in)
		prev := -1
		for i, tok := range r.Tokens {
			if tok.Start < prev {
				t.Errorf("input %q: token %d has Start=%d after a token with Start=%d (order broken)",
					in, i, tok.Start, prev)
			}
			prev = tok.Start
		}
	}
}
