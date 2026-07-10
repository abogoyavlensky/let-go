# re-seq No-Match Semantics Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `re-seq` return `nil` when no match exists, matching Clojure and restoring predicate use in `condp`.

**Tech Stack:** let-go core runtime, Clojure-compatible Lisp tests, Go test runner

---

## Design

The native `re-seq` implementation currently returns `vm.EmptyList` when `FindAllStringSubmatch` finds nothing. Clojure returns `nil`. Although both values are empty sequences, only `nil` is falsey. The distinction breaks predicate contexts such as tools.cli's `(condp re-seq arg ...)`, where the first nonmatching regex currently produces truthy `()` and wins.

Change only the no-match branch to return `vm.NIL`. Preserve the existing list shape for one or more matches, including capture-group vectors. Update the test that encodes the wrong empty-list result and add a regression where `condp re-seq` skips a nonmatching regex and selects a later match.

The motivating verification will rebuild let-go, load `clojure.tools.cli` v1.4.256, and parse a required integer option, a boolean flag, and a positional argument.

## File Structure

- `pkg/rt/lang.go` - return `vm.NIL` from `re-seq` when no matches exist.
- `test/shuffle_reseq_test.lg` - assert Clojure-compatible no-match and `condp` behavior.

### Task 1: Add failing semantic tests

**Files:**
- Modify: `test/shuffle_reseq_test.lg`

- [x] **Step 1: Correct the no-match expectation**
  Replace the empty-list assertion with `nil?`. Add a `condp re-seq` case where `#"^--$"` does not match `"--port"` and `#"^--"` returns `:long`.

- [x] **Step 2: Run the focused test and verify failure**
  Run: `go test ./test -run 'TestRunner/shuffle_reseq_test.lg' -count=1 -v`
  Expected: FAIL because no-match returns `()` and the `condp` chooses the first clause.

### Task 2: Fix re-seq and review

**Files:**
- Modify: `pkg/rt/lang.go`
- Test: `test/shuffle_reseq_test.lg`

- [x] **Step 1: Return nil for no matches**
  Change the `all == nil` branch in native `re-seq` from `vm.EmptyList` to `vm.NIL`.

- [x] **Step 2: Run the focused test**
  Run: `go test ./test -run 'TestRunner/shuffle_reseq_test.lg' -count=1 -v`
  Expected: PASS.

- [x] **Step 3: Run runtime and core tests**
  Run: `go test ./pkg/rt ./test -count=1`
  Expected: PASS.

- [x] **Step 4: Commit the semantic fix**
  Run: `git add pkg/rt/lang.go test/shuffle_reseq_test.lg docs/plans/2026-07-10-re-seq-no-match.md && git commit -m "fix(rt): return nil when re-seq has no matches"`
  Expected: one focused implementation commit with checked task steps.

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
  Expected: no loader error; `parse-opts` returns port `8080`, verbose `true`, positional argument `"input.txt"`, a summary, and `:errors nil`.

- [ ] **Step 4: Complete and commit the plan record**
  Add the completion summary and checked steps, then run: `git add docs/plans/2026-07-10-re-seq-no-match.md && git commit -m "docs: complete re-seq no-match plan"`.
