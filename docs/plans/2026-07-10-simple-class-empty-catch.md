# Simple-Class Empty Catch Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Parse Clojure catch clauses such as `(catch Throwable _)` as a simple class, binding, and empty body while preserving let-go's legacy bare catch syntax.

**Tech Stack:** let-go compiler, Clojure-compatible Lisp tests, Go test runner

---

## Design

`tryCompiler` already distinguishes Clojure's `(catch Class binding body...)` from let-go's legacy `(catch binding body...)`. Qualified or dotted class symbols are unambiguous, and three or more tokens after `catch` provide enough structure to identify a class and binding. The remaining ambiguity is a two-token clause with a simple class name and no body, such as `(catch Throwable _)`.

For this two-token case, treat the first token as a class when it is a simple symbol whose first rune is uppercase. This follows Clojure class naming, supports `Throwable` and `Exception`, and leaves ordinary legacy clauses such as `(catch e :fallback)` unchanged. Qualified classes and Clojure catches with body forms will continue through the existing rules.

Tests will cover empty-body `Throwable` and `Exception` clauses, the existing qualified-class case, a simple-class catch with a body, and legacy bare catch syntax. The motivating verification will load `clojure.tools.cli` v1.4.256 and call `parse-opts` with real options.

## File Structure

- `pkg/compiler/compiler.go` - extend catch-form disambiguation for uppercase simple class symbols with empty bodies.
- `test/catch_class_test.lg` - add regression coverage for simple-class empty-body catches and legacy syntax.

### Task 1: Add failing catch regression tests

**Files:**
- Modify: `test/catch_class_test.lg`

- [x] **Step 1: Add simple-class empty-body tests**
  Assert that `(catch Throwable _)` and `(catch Exception _)` catch a thrown value and return `nil`. Retain coverage for a qualified class, a simple class with a body, and `(catch e :ok)`.

- [x] **Step 2: Run the focused test and verify failure**
  Run: `go test ./test -run 'TestRunner/catch_class_test.lg' -count=1 -v`
  Expected: FAIL while compiling the new cases with `Can't resolve _ in this context`.

### Task 2: Extend catch disambiguation

**Files:**
- Modify: `pkg/compiler/compiler.go`
- Test: `test/catch_class_test.lg`

- [x] **Step 1: Recognize uppercase simple class symbols**
  In the two-token `class + binding` case, classify the first token as a class when its first rune is uppercase. Keep the existing qualified/dotted and token-count rules. Keep lowercase two-token clauses on the legacy `binding + body` path.

- [x] **Step 2: Run the focused test**
  Run: `go test ./test -run 'TestRunner/catch_class_test.lg' -count=1 -v`
  Expected: PASS.

- [x] **Step 3: Run compiler and core tests**
  Run: `go test ./pkg/compiler ./test -count=1`
  Expected: PASS.

- [x] **Step 4: Commit the compiler fix**
  Run: `git add pkg/compiler/compiler.go test/catch_class_test.lg docs/plans/2026-07-10-simple-class-empty-catch.md && git commit -m "fix(compiler): parse empty simple-class catches"`
  Expected: one focused commit containing the implementation, tests, and checked plan steps.

### Task 3: Verify tools.cli and the repository

**Files:**
- No new files.

- [ ] **Step 1: Run all repository tests**
  Run: `go test ./... -count=1`
  Expected: PASS.

- [ ] **Step 2: Build a fresh `lg` binary**
  Run: `make build`
  Expected: PASS and `/Users/andrew/Projects/let-go/lg` rebuilt from the current source.

- [ ] **Step 3: Functionally probe tools.cli**
  Run: `LG_READ_CLJ=1 /Users/andrew/Projects/let-go/lg -source-paths "$HOME/.lgx/gitlibs/github.com/clojure/tools.cli/c24dbcb6c947a547c871f5450b3206517412564d/src/main/clojure" /Users/andrew/Projects/lgx/.tools-cli-probe.lg`
  Expected: no loader error; `parse-opts` returns `{:options {:port 8080 :verbose true}, :arguments ["input.txt"], ... :errors nil}`.

- [ ] **Step 4: Record verification**
  If the probe exposes another independent compatibility gap, stop and report it with a minimal repro. Otherwise mark this plan complete and resume the lgx example.

- [ ] **Step 5: Commit the completed plan record**
  Run: `git add docs/plans/2026-07-10-simple-class-empty-catch.md && git commit -m "docs: complete simple-class catch plan"`
  Expected: the checked steps and completion summary are committed after final verification.
