//go:build !js

/*
 * Copyright (c) 2026 let-go contributors
 * SPDX-License-Identifier: MIT
 */

// Package rt - wasm namespace.
//
// A generic, in-process WebAssembly host backed by the pure-Go wazero
// runtime. Lets let-go code load a .wasm module (from a resource name or
// raw bytes), call its exports, and marshal data through its linear
// memory. WASI (wasi_snapshot_preview1) is enabled, so modules that do
// file I/O work when a directory is mounted via the :dir option.
//
// Build-tagged !js: wazero needs a native host, and let-go's own wasm
// build targets GOOS=js (same boundary as pods.go). Pure Go, so the
// default cgo-free build is preserved.
//
// Surface:
//
//	(wasm/instantiate src [{:dir path} | :dir path])  -> instance handle (boxed)
//	(wasm/call inst "export" & args)      -> result (Int/Float) or vector
//	(wasm/read inst ptr len)              -> bytes
//	(wasm/read-string inst ptr len)       -> string (UTF-8, exact length)
//	(wasm/read-cstring inst ptr)          -> string (UTF-8, NUL-terminated)
//	(wasm/write inst ptr bytes)           -> nil
//	(wasm/close inst)                     -> nil
//
// instantiate never runs guest code: WASI _start is disabled and modules
// with a core WebAssembly start section are rejected. Reactor modules
// therefore must have their init invoked explicitly, e.g.
// (wasm/call inst "_initialize").

package rt

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/nooga/let-go/pkg/vm"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var (
	wasmCtx     = context.Background()
	wasmRuntime wazero.Runtime
	wasmRTMu    sync.Mutex
)

// wasmInstance wraps an instantiated module (and its compiled form, so
// both can be released on close) so it can be boxed as a let-go value.
type wasmInstance struct {
	mod      api.Module
	compiled wazero.CompiledModule
}

// wasmCacheDir returns a writable directory for wazero's compilation cache, or
// "" when none is available (read-only / ephemeral host) — in which case
// modules are recompiled each run rather than cached. The cache holds host-
// native compiled code, so it needs a real writable filesystem; the module
// bytes themselves come from the resource provider (incl. -b embedded bundles)
// and never need one.
func wasmCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(base, "let-go", "wasm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	// Verify the dir is actually writable: an existing but read-only cache dir
	// lets NewCompilationCacheWithDir succeed yet fail later when CompileModule
	// writes an entry — turning a harmless cache miss into a wasm/instantiate
	// error. Probe, and skip the cache if we can't write.
	probe, err := os.CreateTemp(dir, ".w-*")
	if err != nil {
		return ""
	}
	probe.Close()
	os.Remove(probe.Name())
	return dir
}

// wasmRT lazily builds the process-wide runtime with WASI installed and, when a
// writable cache dir exists, a disk compilation cache so compiled modules are
// reused across runs (a large module like SQLite costs ~0.5s to compile cold;
// a cache hit is ~milliseconds). Falls back to no cache if none is writable.
func wasmRT() wazero.Runtime {
	wasmRTMu.Lock()
	defer wasmRTMu.Unlock()
	if wasmRuntime == nil {
		cfg := wazero.NewRuntimeConfig()
		if dir := wasmCacheDir(); dir != "" {
			if cache, err := wazero.NewCompilationCacheWithDir(dir); err == nil {
				cfg = cfg.WithCompilationCache(cache)
			}
		}
		r := wazero.NewRuntimeWithConfig(wasmCtx, cfg)
		wasi_snapshot_preview1.MustInstantiate(wasmCtx, r)
		wasmRuntime = r
	}
	return wasmRuntime
}

func wasmUnboxInstance(v vm.Value) (*wasmInstance, error) {
	b, ok := v.(*vm.Boxed)
	if !ok {
		return nil, fmt.Errorf("expected a wasm instance, got %s", v.Type().Name())
	}
	inst, ok := b.Unbox().(*wasmInstance)
	if !ok {
		return nil, fmt.Errorf("expected a wasm instance, got boxed %T", b.Unbox())
	}
	return inst, nil
}

func wasmAsInt(v vm.Value) (int64, bool) {
	if i, ok := v.(vm.Int); ok {
		return int64(int(i)), true
	}
	return 0, false
}

func wasmAsFloat(v vm.Value) (float64, bool) {
	switch n := v.(type) {
	case vm.Float:
		return float64(n), true
	case vm.Int:
		return float64(int(n)), true
	}
	return 0, false
}

// wasmAsU32 converts a non-negative integer that fits in uint32 (a wasm32
// memory offset or length), rejecting out-of-range values before the cast
// can silently wrap.
func wasmAsU32(v vm.Value) (uint32, error) {
	n, ok := wasmAsInt(v)
	if !ok {
		return 0, fmt.Errorf("expected an integer")
	}
	if n < 0 || n > math.MaxUint32 {
		return 0, fmt.Errorf("value out of uint32 range: %d", n)
	}
	return uint32(n), nil
}

func wasmAsBytes(v vm.Value) ([]byte, bool) {
	switch b := v.(type) {
	case vm.String:
		return []byte(string(b)), true
	case *vm.TypedArray:
		if data, ok := b.Unbox().([]byte); ok {
			return data, true
		}
	}
	return nil, false
}

// wasmEncodeArg encodes a let-go value as a uint64 wasm argument per the
// declared parameter type, sign/range-checking i32 and bit-encoding floats.
func wasmEncodeArg(v vm.Value, t api.ValueType) (uint64, error) {
	switch t {
	case api.ValueTypeI32:
		n, ok := wasmAsInt(v)
		if !ok {
			return 0, fmt.Errorf("expected an integer")
		}
		if n < math.MinInt32 || n > math.MaxUint32 {
			return 0, fmt.Errorf("i32 out of range: %d", n)
		}
		return uint64(uint32(n)), nil
	case api.ValueTypeI64:
		n, ok := wasmAsInt(v)
		if !ok {
			return 0, fmt.Errorf("expected an integer")
		}
		return uint64(n), nil
	case api.ValueTypeF32:
		f, ok := wasmAsFloat(v)
		if !ok {
			return 0, fmt.Errorf("expected a number")
		}
		return api.EncodeF32(float32(f)), nil
	case api.ValueTypeF64:
		f, ok := wasmAsFloat(v)
		if !ok {
			return 0, fmt.Errorf("expected a number")
		}
		return api.EncodeF64(f), nil
	default:
		return 0, fmt.Errorf("unsupported param type 0x%x", t)
	}
}

// wasmDecodeResult decodes a uint64 wasm result per the declared type,
// sign-extending i32 and bit-decoding floats.
func wasmDecodeResult(raw uint64, t api.ValueType) vm.Value {
	switch t {
	case api.ValueTypeI32:
		return vm.Int(int(int32(raw)))
	case api.ValueTypeI64:
		return vm.Int(int(int64(raw)))
	case api.ValueTypeF32:
		return vm.Float(float64(api.DecodeF32(raw)))
	case api.ValueTypeF64:
		return vm.Float(api.DecodeF64(raw))
	default:
		return vm.Int(int(raw))
	}
}

// wasmModuleBytes resolves a resource name (String) via the resource
// provider, or accepts raw bytes.
func wasmModuleBytes(src vm.Value) ([]byte, error) {
	if name, ok := src.(vm.String); ok {
		prov := GetResourceProvider()
		if prov == nil {
			return nil, fmt.Errorf("no resource provider to load %q", string(name))
		}
		rc, ok := prov.Open(string(name))
		if !ok {
			return nil, fmt.Errorf("wasm resource not found: %q", string(name))
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	if b, ok := wasmAsBytes(src); ok {
		return b, nil
	}
	return nil, fmt.Errorf("wasm/instantiate: src must be a resource name or bytes, got %s", src.Type().Name())
}

// wasmHasStartSection reports whether a wasm binary contains a core start
// section (section id 8). wazero runs that function during instantiation
// regardless of ModuleConfig.WithStartFunctions, which would break the
// "instantiate never runs guest code" contract, so such modules are rejected.
func wasmHasStartSection(mod []byte) bool {
	const headerLen = 8 // magic (4) + version (4)
	i := headerLen
	for i < len(mod) {
		id := mod[i]
		i++
		size, n := binary.Uvarint(mod[i:])
		if n <= 0 {
			return false // malformed; let wazero's compiler report it
		}
		i += n
		if id == 8 {
			return true
		}
		i += int(size)
	}
	return false
}

// wasmOptDir extracts the :dir option from the trailing args of
// wasm/instantiate, accepting both a trailing map ({:dir "p"}) and
// trailing keyword/value pairs (:dir "p").
func wasmOptDir(rest []vm.Value) (string, bool) {
	if len(rest) == 1 {
		if l, ok := rest[0].(vm.Lookup); ok {
			if s, ok := l.ValueAt(vm.Keyword("dir")).(vm.String); ok {
				return string(s), true
			}
			return "", false
		}
	}
	for i := 0; i+1 < len(rest); i += 2 {
		if k, ok := rest[i].(vm.Keyword); ok && string(k) == "dir" {
			if s, ok := rest[i+1].(vm.String); ok {
				return string(s), true
			}
		}
	}
	return "", false
}

func wasmWrap(fn func([]vm.Value) (vm.Value, error)) vm.Value {
	v, err := vm.NativeFnType.Wrap(fn)
	if err != nil {
		panic("wasm NS init failed: " + err.Error())
	}
	return v
}

func init() { RegisterInstaller(installWasmNS) }

func installWasmNS() {
	instantiate := wasmWrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) < 1 {
			return vm.NIL, fmt.Errorf("wasm/instantiate: expected (src & opts)")
		}
		bytes, err := wasmModuleBytes(vs[0])
		if err != nil {
			return vm.NIL, err
		}
		if wasmHasStartSection(bytes) {
			return vm.NIL, fmt.Errorf("wasm/instantiate: module has a core start section (would run guest code at instantiation); not supported")
		}
		r := wasmRT()
		compiled, err := r.CompileModule(wasmCtx, bytes)
		if err != nil {
			return vm.NIL, fmt.Errorf("wasm/instantiate: compile: %w", err)
		}
		// WithStartFunctions() disables auto-start: instantiate never runs
		// guest code (no implicit _start), preserving the instantiate-then-call
		// contract. Reactor modules call _initialize explicitly via wasm/call.
		cfg := wazero.NewModuleConfig().WithName("").WithStartFunctions()
		if dir, ok := wasmOptDir(vs[1:]); ok {
			cfg = cfg.WithFSConfig(wazero.NewFSConfig().WithDirMount(dir, "/"))
		}
		mod, err := r.InstantiateModule(wasmCtx, compiled, cfg)
		if err != nil {
			compiled.Close(wasmCtx) //nolint:errcheck
			return vm.NIL, fmt.Errorf("wasm/instantiate: %w", err)
		}
		return vm.NewBoxed(&wasmInstance{mod: mod, compiled: compiled}), nil
	})

	call := wasmWrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) < 2 {
			return vm.NIL, fmt.Errorf("wasm/call: expected (inst fname & args)")
		}
		inst, err := wasmUnboxInstance(vs[0])
		if err != nil {
			return vm.NIL, err
		}
		fname, ok := vs[1].(vm.String)
		if !ok {
			return vm.NIL, fmt.Errorf("wasm/call: fname must be a string")
		}
		fn := inst.mod.ExportedFunction(string(fname))
		if fn == nil {
			return vm.NIL, fmt.Errorf("wasm/call: no exported function %q", string(fname))
		}
		def := fn.Definition()
		ptypes := def.ParamTypes()
		callArgs := vs[2:]
		if len(callArgs) != len(ptypes) {
			return vm.NIL, fmt.Errorf("wasm/call %q: expected %d args, got %d", string(fname), len(ptypes), len(callArgs))
		}
		args := make([]uint64, len(callArgs))
		for i, a := range callArgs {
			enc, err := wasmEncodeArg(a, ptypes[i])
			if err != nil {
				return vm.NIL, fmt.Errorf("wasm/call %q arg %d: %w", string(fname), i, err)
			}
			args[i] = enc
		}
		res, err := fn.Call(wasmCtx, args...)
		if err != nil {
			return vm.NIL, fmt.Errorf("wasm/call %q: %w", string(fname), err)
		}
		rtypes := def.ResultTypes()
		switch len(res) {
		case 0:
			return vm.NIL, nil
		case 1:
			return wasmDecodeResult(res[0], rtypes[0]), nil
		default:
			out := make([]vm.Value, len(res))
			for i, r := range res {
				out[i] = wasmDecodeResult(r, rtypes[i])
			}
			return vm.NewArrayVector(out), nil
		}
	})

	read := wasmWrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) != 3 {
			return vm.NIL, fmt.Errorf("wasm/read: expected (inst ptr len)")
		}
		inst, err := wasmUnboxInstance(vs[0])
		if err != nil {
			return vm.NIL, err
		}
		ptr, err := wasmAsU32(vs[1])
		if err != nil {
			return vm.NIL, fmt.Errorf("wasm/read: ptr: %w", err)
		}
		n, err := wasmAsU32(vs[2])
		if err != nil {
			return vm.NIL, fmt.Errorf("wasm/read: len: %w", err)
		}
		mem := inst.mod.Memory()
		if mem == nil {
			return vm.NIL, fmt.Errorf("wasm/read: module has no exported memory")
		}
		buf, ok := mem.Read(ptr, n)
		if !ok {
			return vm.NIL, fmt.Errorf("wasm/read: out of range (ptr=%d len=%d)", ptr, n)
		}
		cp := make([]byte, len(buf))
		copy(cp, buf)
		return vm.NewByteArrayFrom(cp), nil
	})

	write := wasmWrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) != 3 {
			return vm.NIL, fmt.Errorf("wasm/write: expected (inst ptr bytes)")
		}
		inst, err := wasmUnboxInstance(vs[0])
		if err != nil {
			return vm.NIL, err
		}
		ptr, err := wasmAsU32(vs[1])
		if err != nil {
			return vm.NIL, fmt.Errorf("wasm/write: ptr: %w", err)
		}
		b, ok := wasmAsBytes(vs[2])
		if !ok {
			return vm.NIL, fmt.Errorf("wasm/write: 3rd arg must be bytes or string")
		}
		mem := inst.mod.Memory()
		if mem == nil {
			return vm.NIL, fmt.Errorf("wasm/write: module has no exported memory")
		}
		if !mem.Write(ptr, b) {
			return vm.NIL, fmt.Errorf("wasm/write: out of range (ptr=%d len=%d)", ptr, len(b))
		}
		return vm.NIL, nil
	})

	closeFn := wasmWrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) != 1 {
			return vm.NIL, fmt.Errorf("wasm/close: expected (inst)")
		}
		inst, err := wasmUnboxInstance(vs[0])
		if err != nil {
			return vm.NIL, err
		}
		cerr := inst.mod.Close(wasmCtx)
		if e := inst.compiled.Close(wasmCtx); e != nil && cerr == nil {
			cerr = e
		}
		if cerr != nil {
			return vm.NIL, cerr
		}
		return vm.NIL, nil
	})

	readString := wasmWrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) != 3 {
			return vm.NIL, fmt.Errorf("wasm/read-string: expected (inst ptr len)")
		}
		inst, err := wasmUnboxInstance(vs[0])
		if err != nil {
			return vm.NIL, err
		}
		ptr, err := wasmAsU32(vs[1])
		if err != nil {
			return vm.NIL, fmt.Errorf("wasm/read-string: ptr: %w", err)
		}
		n, err := wasmAsU32(vs[2])
		if err != nil {
			return vm.NIL, fmt.Errorf("wasm/read-string: len: %w", err)
		}
		mem := inst.mod.Memory()
		if mem == nil {
			return vm.NIL, fmt.Errorf("wasm/read-string: module has no exported memory")
		}
		buf, ok := mem.Read(ptr, n)
		if !ok {
			return vm.NIL, fmt.Errorf("wasm/read-string: out of range (ptr=%d len=%d)", ptr, n)
		}
		return vm.String(string(buf)), nil // string() copies; safe vs the memory view
	})

	readCString := wasmWrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) != 2 {
			return vm.NIL, fmt.Errorf("wasm/read-cstring: expected (inst ptr)")
		}
		inst, err := wasmUnboxInstance(vs[0])
		if err != nil {
			return vm.NIL, err
		}
		ptr, err := wasmAsU32(vs[1])
		if err != nil {
			return vm.NIL, fmt.Errorf("wasm/read-cstring: ptr: %w", err)
		}
		mem := inst.mod.Memory()
		if mem == nil {
			return vm.NIL, fmt.Errorf("wasm/read-cstring: module has no exported memory")
		}
		var out []byte
		for off := ptr; ; off++ {
			b, ok := mem.ReadByte(off)
			if !ok {
				return vm.NIL, fmt.Errorf("wasm/read-cstring: unterminated string at ptr=%d", ptr)
			}
			if b == 0 {
				break
			}
			out = append(out, b)
		}
		return vm.String(string(out)), nil
	})

	ns := vm.NewNamespace("wasm")
	ns.Def("instantiate", instantiate)
	ns.Def("call", call)
	ns.Def("read", read)
	ns.Def("read-string", readString)
	ns.Def("read-cstring", readCString)
	ns.Def("write", write)
	ns.Def("close", closeFn)
	RegisterNS(ns)
}
