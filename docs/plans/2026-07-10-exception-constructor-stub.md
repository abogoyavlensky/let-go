# Exception Constructor Stub Implementation Plan

**Status:** Completed 2026-07-10

> **For agentic workers:** Use executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let JVM-oriented Clojure source compile references to `Exception.` while failing loudly if that unavailable constructor is called under let-go.

**Tech Stack:** let-go runtime compatibility aliases, Clojure-compatible Lisp tests, Go test runner

---

## Design

The compiler rewrites `(Exception. args...)` to `(->Exception args...)`. Add a single native stub under both `Exception.` and `->Exception` in `installClojureCompatAliases`, beside the existing compile-only `java.util.ArrayList` constructor stub. The aliases let namespaces containing the constructor compile without claiming that let-go implements Java exception objects.

The stub will always return an error with the message `Exception. is unavailable under let-go`. This makes every runtime call fail clearly. It will not return a marker or a plausible substitute that downstream code could mistake for a real throwable.

Tests will define a function containing `(Exception. "boom")`, which proves compile-time resolution, then call it and assert the explicit failure message. The motivating verification will load `clojure.tools.cli` v1.4.256 and call `parse-opts` on a required integer option, a boolean flag, and a positional argument.

## File Structure

- `pkg/rt/lang.go` - register compile-only `Exception.` and `->Exception` aliases.
- `test/clojure_compat_aliases_test.lg` - verify compile-time resolution and loud runtime failure.

### Task 1: Add failing constructor-stub tests

**Files:**
- Modify: `test/clojure_compat_aliases_test.lg`

- [x] **Step 1: Add a function that references `Exception.`**
  Define a function whose body constructs `(Exception. "boom")`. Add a test that calls the function, catches the failure, and expects `Exception. is unavailable under let-go`.

- [x] **Step 2: Run the focused test and verify failure**
  Run: `go test ./test -run 'TestRunner/clojure_compat_aliases_test.lg' -count=1 -v`
  Expected: FAIL while compiling the test with `Can't resolve ->Exception in this context`.

### Task 2: Register the compile-only aliases

**Files:**
- Modify: `pkg/rt/lang.go`
- Test: `test/clojure_compat_aliases_test.lg`

- [x] **Step 1: Add the loud constructor stub**
  Create one native function in `installClojureCompatAliases` that always returns `Exception. is unavailable under let-go`. Register it as both `Exception.` and `->Exception`.

- [x] **Step 2: Run the focused test**
  Run: `go test ./test -run 'TestRunner/clojure_compat_aliases_test.lg' -count=1 -v`
  Expected: PASS.

- [x] **Step 3: Run runtime and core tests**
  Run: `go test ./pkg/rt ./test -count=1`
  Expected: PASS.

- [x] **Step 4: Commit the runtime fix**
  Run: `git add pkg/rt/lang.go test/clojure_compat_aliases_test.lg docs/plans/2026-07-10-exception-constructor-stub.md && git commit -m "fix(rt): add compile-only Exception constructor"`
  Expected: one focused commit with implementation, tests, and checked plan steps.

### Task 3: Verify tools.cli and the repository

**Files:**
- No new files.

- [x] **Step 1: Run all repository tests**
  Run: `go test ./... -count=1`
  Expected: PASS.

- [x] **Step 2: Build a fresh `lg` binary**
  Run: `make build`
  Expected: PASS and `/Users/andrew/Projects/let-go/lg` rebuilt from the current source.

- [x] **Step 3: Functionally probe tools.cli**
  Run: `LG_READ_CLJ=1 /Users/andrew/Projects/let-go/lg -source-paths "$HOME/.lgx/gitlibs/github.com/clojure/tools.cli/c24dbcb6c947a547c871f5450b3206517412564d/src/main/clojure" /Users/andrew/Projects/lgx/.tools-cli-probe.lg`
  Expected: no loader error; `parse-opts` returns port `8080`, verbose `true`, positional argument `"input.txt"`, a summary, and `:errors nil`.

- [x] **Step 4: Complete and commit the plan record**
  Add the completion summary and checked steps, then run: `git add docs/plans/2026-07-10-exception-constructor-stub.md && git commit -m "docs: complete Exception constructor plan"`.

## Completion Summary

Registered `Exception.` and `->Exception` as one compile-only native stub. Namespaces containing the JVM constructor now compile, while runtime calls fail with `Exception. is unavailable under let-go`. Focused tests, runtime tests, the full repository suite, and Codex review pass.

The tools.cli probe now loads the full namespace and exposes the next independent gap: `re-seq` returns truthy `()` instead of `nil` when no match exists, so `condp re-seq` tokenization chooses the wrong clause. That stdlib behavior requires a separate plan.

Deviations: none.

What the plan could have specified better: nothing.
