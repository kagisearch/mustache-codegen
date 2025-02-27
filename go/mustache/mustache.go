// Copyright (c) 2025 Kagi Search
// SPDX-License-Identifier: MIT

// Package mustache provides runtime support for template functions generated by mustache-codegen.
package mustache

import (
	"fmt"
	"iter"
	"math"
	"reflect"
	"strings"
)

// Lookup finds the last value for the given key (possibly containing dots).
// in the contextStack.
// A key will be looked up as either a map key or a struct field
// after dereferencing all pointers and interfaces.
func Lookup(contextStack []reflect.Value, path string) reflect.Value {
	if path == "." {
		return contextStack[len(contextStack)-1]
	}
	parts := strings.Split(path, ".")

	// Look through context stack for first part.
	var v reflect.Value
	for i := len(contextStack) - 1; i >= 0; i-- {
		v = property(contextStack[i], parts[0])
		if v.IsValid() {
			break
		}
	}

	for _, pname := range parts[1:] {
		v = property(v, pname)
	}
	return v
}

func property(v reflect.Value, k string) reflect.Value {
	v = resolve(v)
	switch v.Kind() {
	case reflect.Struct:
		return v.FieldByName(k)
	case reflect.Map:
		ktype := v.Type().Key()
		if ktype.Kind() != reflect.String {
			return reflect.Value{}
		}
		k := reflect.ValueOf(k).Convert(ktype)
		return v.MapIndex(k)
	default:
		return reflect.Value{}
	}
}

// ToString converts a [reflect.Value] to a string representation
// using [fmt.Sprint].
// Pointers and interfaces will be dereferenced first.
// Nil or invalid values will be converted to the empty string.
func ToString(v reflect.Value) string {
	v = resolve(v)
	if !v.IsValid() {
		return ""
	}
	if k := v.Kind(); (k == reflect.Pointer || k == reflect.Interface) && v.IsNil() {
		return ""
	}
	return fmt.Sprint(v)
}

// IsFalsyOrEmptyList reports whether v is invalid, nil, false, an empty string, or an empty list.
// Pointers and interfaces will be dereferenced first.
func IsFalsyOrEmptyList(v reflect.Value) bool {
	v = resolve(v)
	if !v.IsValid() {
		return true
	}

	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.String:
		return v.String() == ""
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		return f == 0 || math.IsNaN(f)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Array, reflect.Slice:
		return v.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

// ForEach returns an iterator over v's elements
// if v represents a slice or an array,
// or an iterator that only yields v otherwise.
// Pointers and interfaces will be dereferenced first.
func ForEach(v reflect.Value) iter.Seq[reflect.Value] {
	return func(yield func(reflect.Value) bool) {
		v := resolve(v)
		if !IsFalsyOrEmptyList(v) {
			switch v.Kind() {
			case reflect.Array, reflect.Slice:
				for i := range v.Len() {
					if !yield(v.Index(i)) {
						return
					}
				}
			default:
				yield(v)
			}
		}
	}
}

func resolve(v reflect.Value) reflect.Value {
	for {
		k := v.Kind()
		if k != reflect.Pointer && k != reflect.Interface || v.IsNil() {
			return v
		}
		v = v.Elem()
	}
}
