package van

import (
	"context"
	"reflect"
)

var (
	typeVan     = reflect.TypeOf((*Van)(nil)).Elem()
	typeError   = reflect.TypeOf((*error)(nil)).Elem()
	typeContext = reflect.TypeOf((*context.Context)(nil)).Elem()
)

func isStructPtr(t reflect.Type) bool {
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct
}
