//go:build !js

package rt

import (
	"testing"

	"github.com/nooga/let-go/pkg/vm"
)

// addWasm is a minimal module, hand-assembled (wat2wasm unavailable). The
// tests validate it by instantiating and calling — a wrong byte fails loudly.
// WAT:
//
//	(module
//	  (func (export "add") (param i32 i32) (result i32)
//	    local.get 0 local.get 1 i32.add)
//	  (memory (export "memory") 1))
var addWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // header
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f, // type: (i32,i32)->i32
	0x03, 0x02, 0x01, 0x00, // func section: 1 func, type 0
	0x05, 0x03, 0x01, 0x00, 0x01, // memory section: 1 page
	// export section: "add" (func 0), "memory" (mem 0)
	0x07, 0x10, 0x02, 0x03, 0x61, 0x64, 0x64, 0x00, 0x00,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
	// code section: local.get 0; local.get 1; i32.add; end
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b,
}

func wasmFn(t *testing.T, name string) vm.Fn {
	t.Helper()
	v := NS("wasm").Lookup(vm.Symbol(name))
	fn, ok := v.(vm.Fn)
	if !ok {
		t.Fatalf("wasm/%s is not a fn (got %T)", name, v)
	}
	return fn
}

func TestWasmInstantiateCall(t *testing.T) {
	inst, err := wasmFn(t, "instantiate").Invoke([]vm.Value{vm.NewByteArrayFrom(addWasm)})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	res, err := wasmFn(t, "call").Invoke([]vm.Value{inst, vm.String("add"), vm.Int(2), vm.Int(3)})
	if err != nil {
		t.Fatalf("call add: %v", err)
	}
	if i, ok := res.(vm.Int); !ok || int(i) != 5 {
		t.Fatalf("add(2,3): expected 5, got %v (%T)", res, res)
	}
	if _, err := wasmFn(t, "close").Invoke([]vm.Value{inst}); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestWasmMemory(t *testing.T) {
	inst, err := wasmFn(t, "instantiate").Invoke([]vm.Value{vm.NewByteArrayFrom(addWasm)})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer wasmFn(t, "close").Invoke([]vm.Value{inst}) //nolint:errcheck

	want := []byte("hello")
	if _, err := wasmFn(t, "write").Invoke([]vm.Value{inst, vm.Int(16), vm.NewByteArrayFrom(want)}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := wasmFn(t, "read").Invoke([]vm.Value{inst, vm.Int(16), vm.Int(len(want))})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	ta, ok := got.(*vm.TypedArray)
	if !ok {
		t.Fatalf("read returned %T, want *vm.TypedArray", got)
	}
	if b, _ := ta.Unbox().([]byte); string(b) != "hello" {
		t.Fatalf("read back %q, want %q", b, "hello")
	}
}

func TestWasmResourceAndDir(t *testing.T) {
	prev := GetResourceProvider()
	SetResourceProvider(NewFSResourceProvider([]string{"testdata"}))
	defer SetResourceProvider(prev)

	// load a module by resource name (resolved via the resource provider)
	inst, err := wasmFn(t, "instantiate").Invoke([]vm.Value{vm.String("add.wasm")})
	if err != nil {
		t.Fatalf("instantiate from resource: %v", err)
	}
	res, err := wasmFn(t, "call").Invoke([]vm.Value{inst, vm.String("add"), vm.Int(20), vm.Int(22)})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if i, ok := res.(vm.Int); !ok || int(i) != 42 {
		t.Fatalf("add(20,22): expected 42, got %v (%T)", res, res)
	}
	if _, err := wasmFn(t, "close").Invoke([]vm.Value{inst}); err != nil {
		t.Fatalf("close: %v", err)
	}

	// :dir option (keyword/value form) is accepted and mounts without error
	inst2, err := wasmFn(t, "instantiate").Invoke([]vm.Value{
		vm.String("add.wasm"), vm.Keyword("dir"), vm.String(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("instantiate with :dir: %v", err)
	}
	wasmFn(t, "close").Invoke([]vm.Value{inst2}) //nolint:errcheck
}

func TestWasmReadString(t *testing.T) {
	inst, err := wasmFn(t, "instantiate").Invoke([]vm.Value{vm.NewByteArrayFrom(addWasm)})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer wasmFn(t, "close").Invoke([]vm.Value{inst}) //nolint:errcheck

	// "café" is 5 UTF-8 bytes (é = 2 bytes); a per-byte decode would mangle it,
	// so this proves correct UTF-8 handling. Trailing NUL + junk for read-cstring.
	if _, err := wasmFn(t, "write").Invoke([]vm.Value{inst, vm.Int(0), vm.String("café\x00ZZ")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := wasmFn(t, "read-string").Invoke([]vm.Value{inst, vm.Int(0), vm.Int(5)})
	if err != nil {
		t.Fatalf("read-string: %v", err)
	}
	if s, ok := got.(vm.String); !ok || string(s) != "café" {
		t.Fatalf("read-string: want \"café\", got %v (%T)", got, got)
	}
	got2, err := wasmFn(t, "read-cstring").Invoke([]vm.Value{inst, vm.Int(0)})
	if err != nil {
		t.Fatalf("read-cstring: %v", err)
	}
	if s, ok := got2.(vm.String); !ok || string(s) != "café" {
		t.Fatalf("read-cstring: want \"café\", got %v (%T)", got2, got2)
	}
}
