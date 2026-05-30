/*
 * Copyright (c) 2021 Marcin Gasperowicz <xnooga@gmail.com>
 * SPDX-License-Identifier: MIT
 */

package vm

import (
	"context"
	"fmt"
	"reflect"
)

type theChanType struct {
}

func (t *theChanType) String() string  { return t.Name() }
func (t *theChanType) Type() ValueType { return TypeType }
func (t *theChanType) Unbox() any      { return reflect.TypeFor[*theChanType]() }

func (t *theChanType) Name() string { return "let-go.lang.Chan" }
func (t *theChanType) Box(b any) (Value, error) {
	chv := reflect.ValueOf(b)
	if chv.Type().Kind() != reflect.Chan {
		return NIL, NewTypeError(b, "is not a channel", t)
	}
	rb := make(Chan)
	Goroutines.Go(func(ctx context.Context) {
		for {
			v, ok := chv.Recv()
			if !ok {
				break
			}
			bv, _ := BoxValue(v)
			// Forward-send is cancellable via the registry. (reflect
			// chv.Recv() above is not ctx-aware, so a cancel won't
			// interrupt a goroutine blocked waiting on the source Go
			// channel — only one blocked forwarding a value.)
			select {
			case rb <- bv:
			case <-ctx.Done():
				return
			}
		}
		close(rb)
	})
	return Chan(rb), nil
}

// Chan is either TRUE or FALSE
type Chan chan Value

// Type implements Value
func (n Chan) Type() ValueType { return ChanType }

// Unbox implements Value
func (n Chan) Unbox() any { return n }

// ChanType is the type of Chan
var ChanType *theChanType = &theChanType{}

func (n Chan) String() string {
	return fmt.Sprintf("<chan %p>", n)
}
