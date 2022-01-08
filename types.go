package van

import (
	"context"
	"reflect"
)

var (
	vanType     = reflect.TypeOf((*Van)(nil)).Elem()
	errorType   = reflect.TypeOf((*error)(nil)).Elem()
	contextType = reflect.TypeOf((*context.Context)(nil)).Elem()
)

func isBusItself(t reflect.Type) bool {
	return t == vanType
}

func isStructPtr(t reflect.Type) bool {
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct
}

func isContext(t reflect.Type) bool {
	return t.Implements(contextType)
}

func isError(t reflect.Type) bool {
	return t.Implements(errorType)
}
