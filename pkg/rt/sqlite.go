//go:build sqlite

/*
 * Copyright (c) 2026 let-go contributors
 * SPDX-License-Identifier: MIT
 */

// Package rt - sqlite namespace.
//
// In-process SQLite backed by the pure-Go modernc.org/sqlite driver.
// Compiled in only with `-tags sqlite`; without the tag this file (and
// the modernc dependency) is excluded from the build, so the default
// `lg` binary stays cgo-free and small.
//
// Surface:
//
//	(sqlite/open path)            -> db handle (boxed *sql.DB)
//	(sqlite/execute! db sql & ps) -> {:rows-affected n :last-insert-id m}
//	(sqlite/query db sql & ps)    -> [{:col val ...} ...]   (keywordized cols)
//	(sqlite/close db)             -> nil

package rt

import (
	"database/sql"
	"fmt"

	"github.com/nooga/let-go/pkg/vm"

	_ "modernc.org/sqlite"
)

func init() { RegisterInstaller(installSqliteNS) }

// unboxDB extracts the *sql.DB from a boxed handle argument.
func unboxDB(v vm.Value) (*sql.DB, error) {
	b, ok := v.(*vm.Boxed)
	if !ok {
		return nil, fmt.Errorf("expected a sqlite db handle, got %s", v.Type().Name())
	}
	db, ok := b.Unbox().(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("expected a sqlite db handle, got boxed %T", b.Unbox())
	}
	return db, nil
}

// params converts trailing let-go args to Go values for query binding.
func params(vs []vm.Value) []any {
	out := make([]any, len(vs))
	for i := range vs {
		out[i] = vs[i].Unbox()
	}
	return out
}

// sqlToValue maps a scanned SQL column value to a let-go Value.
func sqlToValue(v any) vm.Value {
	switch x := v.(type) {
	case nil:
		return vm.NIL
	case int64:
		return vm.Int(int(x))
	case float64:
		return vm.Float(x)
	case bool:
		return vm.Boolean(x)
	case string:
		return vm.String(x)
	case []byte:
		return vm.String(string(x))
	default:
		return vm.String(fmt.Sprintf("%v", x))
	}
}

func installSqliteNS() {
	open, err := vm.NativeFnType.Wrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) != 1 {
			return vm.NIL, fmt.Errorf("sqlite/open: expected (path)")
		}
		path, ok := vs[0].(vm.String)
		if !ok {
			return vm.NIL, fmt.Errorf("sqlite/open: path must be a string")
		}
		db, err := sql.Open("sqlite", string(path))
		if err != nil {
			return vm.NIL, err
		}
		if err := db.Ping(); err != nil {
			return vm.NIL, err
		}
		return vm.NewBoxed(db), nil
	})

	execute, err2 := vm.NativeFnType.Wrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) < 2 {
			return vm.NIL, fmt.Errorf("sqlite/execute!: expected (db sql & params)")
		}
		db, err := unboxDB(vs[0])
		if err != nil {
			return vm.NIL, err
		}
		q, ok := vs[1].(vm.String)
		if !ok {
			return vm.NIL, fmt.Errorf("sqlite/execute!: sql must be a string")
		}
		res, err := db.Exec(string(q), params(vs[2:])...)
		if err != nil {
			return vm.NIL, err
		}
		affected, _ := res.RowsAffected()
		lastID, _ := res.LastInsertId()
		m := vm.EmptyPersistentMap
		m = m.Assoc(vm.Keyword("rows-affected"), vm.Int(int(affected))).(*vm.PersistentMap)
		m = m.Assoc(vm.Keyword("last-insert-id"), vm.Int(int(lastID))).(*vm.PersistentMap)
		return m, nil
	})

	query, err3 := vm.NativeFnType.Wrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) < 2 {
			return vm.NIL, fmt.Errorf("sqlite/query: expected (db sql & params)")
		}
		db, err := unboxDB(vs[0])
		if err != nil {
			return vm.NIL, err
		}
		q, ok := vs[1].(vm.String)
		if !ok {
			return vm.NIL, fmt.Errorf("sqlite/query: sql must be a string")
		}
		rows, err := db.Query(string(q), params(vs[2:])...)
		if err != nil {
			return vm.NIL, err
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return vm.NIL, err
		}
		var out []vm.Value
		for rows.Next() {
			cells := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range cells {
				ptrs[i] = &cells[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return vm.NIL, err
			}
			m := vm.EmptyPersistentMap
			for i, c := range cols {
				m = m.Assoc(vm.Keyword(c), sqlToValue(cells[i])).(*vm.PersistentMap)
			}
			out = append(out, m)
		}
		if err := rows.Err(); err != nil {
			return vm.NIL, err
		}
		return vm.NewArrayVector(out), nil
	})

	closeDB, err4 := vm.NativeFnType.Wrap(func(vs []vm.Value) (vm.Value, error) {
		if len(vs) != 1 {
			return vm.NIL, fmt.Errorf("sqlite/close: expected (db)")
		}
		db, err := unboxDB(vs[0])
		if err != nil {
			return vm.NIL, err
		}
		return vm.NIL, db.Close()
	})

	if err != nil || err2 != nil || err3 != nil || err4 != nil {
		panic("sqlite NS init failed")
	}

	ns := vm.NewNamespace("sqlite")
	ns.Def("open", open)
	ns.Def("execute!", execute)
	ns.Def("query", query)
	ns.Def("close", closeDB)
	RegisterNS(ns)
}
