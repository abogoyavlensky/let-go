/*
 * Copyright (c) 2026 Norman Nunley, Jr <nnunley@gmail.com>
 * Part of the let-go project; see CONTRIBUTORS for full list of authors.
 * SPDX-License-Identifier: MIT
 */

package vm

import "testing"

// TestFormSourceReset pins the leak fix: FormSource is an append-only,
// identity-keyed map. Reset must empty it and release the form keys so a
// re-compiling loop (BenchmarkClojureTestSuite) does not accumulate a
// live entry per form ever read.
func TestFormSourceReset(t *testing.T) {
	fs := &formSourceMap{m: map[any]*SourceInfo{}}

	l1 := NewList([]Value{Int(1)})
	l2 := NewList([]Value{Int(2)})
	fs.Set(l1, SourceInfo{Line: 1})
	fs.Set(l2, SourceInfo{Line: 2})
	if fs.Len() != 2 {
		t.Fatalf("expected 2 entries before reset, got %d", fs.Len())
	}
	if fs.Get(l1) == nil {
		t.Fatal("expected source info for l1 before reset")
	}

	fs.Reset()

	if fs.Len() != 0 {
		t.Fatalf("expected 0 entries after reset, got %d", fs.Len())
	}
	if fs.Get(l1) != nil {
		t.Fatal("expected nil source info for l1 after reset")
	}

	// Still usable after reset.
	fs.Set(l1, SourceInfo{Line: 3})
	if got := fs.Get(l1); got == nil || got.Line != 3 {
		t.Fatalf("expected reusable map after reset, got %+v", got)
	}
}
