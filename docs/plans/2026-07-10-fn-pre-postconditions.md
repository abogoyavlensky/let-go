# Function Preconditions and Postconditions Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make let-go's `fn` and `defn` macros implement Clojure-compatible `:pre` and `:post` condition maps, including the postcondition `%` result binding required by `clojure.tools.cli`.

**Tech Stack:** let-go core macros, Clojure-compatible Lisp, Go test runner, generated runtime bundle

---

## Design

The shared `fn-expand` helper will recognize a condition map at the start of an arity body. It will expand each precondition into an `assert` before evaluating the body. For postconditions, it will evaluate the body once, bind the result to `%`, run each postcondition assertion in that binding, and return `%`. This matches Clojure's observable expansion and prevents repeated body side effects.

Condition expansion will happen before the existing destructuring expansion wraps the body. As a result, preconditions and postconditions can refer to destructured locals, as they can in Clojure. The shared helper already serves anonymous, named, single-arity, and multi-arity functions, so the change belongs there rather than in `defn` alone.

Tests will cover successful conditions, thrown assertion failures, `%`, single body evaluation, destructured parameters, and multi-arity functions. The implementation will use let-go's existing `assert` macro and preserve behavior for functions without a condition map.

After changing `pkg/rt/core/core.lg`, regenerate the runtime bundle before running tests or rebuilding `lg`. A final external smoke test will load `clojure.tools.cli` v1.4.256 from `src/main/clojure` and call `parse-opts`; this proves the motivating library loads fully rather than merely registering a partial namespace.

## File Structure

- `pkg/rt/core/core.lg` - expand `:pre` and `:post` condition maps in the shared `fn-expand` helper.
- `test/fn_prepost_test.lg` - exercise condition semantics through let-go's Lisp test runner.
- Generated runtime bundle files selected by `make generate` - keep compiled stdlib artifacts synchronized with `core.lg`.

### Task 1: Add failing function-condition tests

**Files:**
- Create: `test/fn_prepost_test.lg`

- [x] **Step 1: Write focused condition tests**
  Add tests for a passing precondition, a failing precondition, a passing postcondition that uses `%`, a failing postcondition, a side-effecting body evaluated once, destructured argument locals in conditions, multi-arity conditions, and an ordinary function without a condition map.

- [x] **Step 2: Run the focused test and verify failure**
  Run: `go test ./test -run 'TestRunner/fn_prepost_test.lg' -count=1 -v`
  Expected: FAIL while compiling the test, with `%` reported as unresolved or condition maps treated as executable values.

### Task 2: Implement condition expansion

> Deviation: Treat a leading map as a condition map only when another body form follows it. A lone map is the function's return value, matching Clojure and preserving existing map-returning functions.

> Deviation: Codex review found that unqualified generated forms could be captured and the reader conflated `%` with `%1`. Qualify generated core macros and restrict `%` shorthand translation to `#(...)` so postconditions remain hygienic.

**Files:**
- Modify: `pkg/rt/core/core.lg`
- Test: `test/fn_prepost_test.lg`

- [x] **Step 1: Expand the condition map in `fn-expand`**
  Detect a leading map in the arity body. Convert each `:pre` expression to an `assert` before the user body. When `:post` is present, wrap the user body in a single `let` binding named `%`, append one `assert` per postcondition, and return `%`. Feed that expanded body into the existing destructuring and keyword-argument paths so conditions see destructured locals.

- [x] **Step 2: Regenerate the runtime bundle**
  Run: `make generate`
  Expected: PASS and generated stdlib artifacts updated to match `pkg/rt/core/core.lg`.

- [x] **Step 3: Run the focused test**
  Run: `go test ./test -run 'TestRunner/fn_prepost_test.lg' -count=1 -v`
  Expected: PASS.

- [x] **Step 4: Run the core test package**
  Run: `go test ./test -count=1`
  Expected: PASS.

- [x] **Step 5: Commit the implementation**
  Run: `git add pkg/rt/core/core.lg test/fn_prepost_test.lg pkg/rt/generated.sums pkg/rt/core.lgb && git commit -m "fix(core): support function pre and postconditions"`
  If `make generate` changes a different tracked generated-file set, stage that exact set instead of assuming both generated paths above exist.

### Task 3: Verify the motivating library and full repository

**Files:**
- No new files.

- [ ] **Step 1: Run all repository tests**
  Run: `go test ./... -count=1`
  Expected: PASS.

- [ ] **Step 2: Build a fresh `lg` binary**
  Run: `make build`
  Expected: PASS and `/Users/andrew/Projects/let-go/lg` rebuilt from the current source.

- [ ] **Step 3: Functionally probe tools.cli**
  Run a temporary `.lg` file with `LG_READ_CLJ=1`, `-source-paths "$HOME/.lgx/gitlibs/github.com/clojure/tools.cli/c24dbcb6c947a547c871f5450b3206517412564d/src/main/clojure"`, and an `ns` form requiring `[clojure.tools.cli :as cli]`. Call `cli/parse-opts` with a required integer option, a boolean flag, and a positional argument.
  Expected: no loader error; the result contains parsed options, the positional argument, a formatted summary, and `:errors nil`.

- [ ] **Step 4: Record verification in the implementation commit if needed**
  If verification exposes a defect, fix it with a failing regression test and amend the implementation commit. Otherwise leave the verified commit unchanged.
