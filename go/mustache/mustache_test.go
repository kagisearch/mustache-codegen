package mustache

import (
	"reflect"
	"testing"
)

func TestLookup(t *testing.T) {
	contextStack := []reflect.Value{reflect.ValueOf(map[string]any{
		"subject": "world"})}
	if got := Lookup(contextStack, "subject"); got.Interface() != "world" {
		t.Errorf("Lookup(...) = %#v", got)
	}
}
