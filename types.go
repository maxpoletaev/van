package van

import (
	"context"
	"fmt"
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

func validateProviderSignature(t reflect.Type) error {
	switch {
	case t.Kind() != reflect.Func:
		return errInvalidType.fmt("provider must be a function, got %s", t.String())
	case t.NumOut() != 2:
		return errInvalidType.fmt("provider must have two return values, got %d", t.NumOut())
	case t.Out(0).Kind() != reflect.Interface:
		return errInvalidType.fmt("provider's first return value must be an interface, got %s", t.Out(0).String())
	case !t.Out(1).Implements(typeError):
		return errInvalidType.fmt("provider's second return value must be an error, got %s", t.Out(1).String())
	}

	if err := validateDependencyArgs(t, 0); err != nil {
		return err
	}

	return nil
}

func validateHandlerSignature(t reflect.Type) error {
	switch {
	case t.Kind() != reflect.Func:
		return errInvalidType.fmt("handler must be a function, got %s", t.String())
	case t.NumIn() < 2:
		return errInvalidType.fmt("handler must have at least 2 arguments, got %s", fmt.Sprint(t.NumIn()))
	case t.In(0) != typeContext:
		return errInvalidType.fmt("handler's first argument must be context.Context, got %s", t.In(0).String())
	case !isStructPtr(t.In(1)):
		return errInvalidType.fmt("handler's second argument must be a struct pointer, got %s", t.In(1).String())
	case t.NumOut() != 1:
		return errInvalidType.fmt("handler must have one return value, got %s", fmt.Sprint(t.NumOut()))
	case !t.Out(0).Implements(typeError):
		return errInvalidType.fmt("handler's return type must be error, got %s", t.Out(0).String())
	}

	if err := validateDependencyArgs(t, 2); err != nil {
		return err
	}

	return nil
}

func validateListenerSignature(t reflect.Type) error {
	switch {
	case t.Kind() != reflect.Func:
		return errInvalidType.fmt("handler must be a function, got %s", t.String())
	case t.NumIn() < 2:
		return errInvalidType.fmt("handler must have at least 2 arguments, got %s", fmt.Sprint(t.NumIn()))
	case t.In(0) != typeContext:
		return errInvalidType.fmt("handler's first argument must be context.Context, got %s", t.In(0).String())
	case t.In(1).Kind() != reflect.Struct:
		return errInvalidType.fmt("handler's second argument must be a struct, got %s", t.In(1).String())
	case t.NumOut() != 0:
		return errInvalidType.fmt("event handler should not have any return values")
	}

	if err := validateDependencyArgs(t, 2); err != nil {
		return err
	}

	return nil
}

func validateExecLambdaSignature(t reflect.Type) error {
	switch {
	case t.Kind() != reflect.Func:
		return errInvalidType.fmt("fn should be a function, got %s", t.String())
	case t.NumOut() != 1:
		return errInvalidType.fmt("fn must have one return value, got %s", fmt.Sprint(t.NumOut()))
	case !t.Out(0).Implements(typeError):
		return errInvalidType.fmt("return value must be an error, got %s", t.Out(0).String())
	}

	if err := validateDependencyArgs(t, 0); err != nil {
		return err
	}

	return nil
}

func validateDependencyArgs(t reflect.Type, start int) error {
	for i := start; i < t.NumIn(); i++ {
		argType := t.In(i)

		switch argType.Kind() {
		case reflect.Interface:
			continue
		case reflect.Struct:
			if err := validateDependencyStruct(argType); err != nil {
				return fmt.Errorf("error in dependency struct argument %d: %w", i, err)
			}

			continue
		default:
			return errInvalidType.fmt("argument %d must be an interface or a struct, got %s", i, argType.String())
		}
	}

	return nil
}

func validateDependencyStruct(t reflect.Type) error {
	for _, f := range reflect.VisibleFields(t) {
		if !f.IsExported() {
			return errInvalidType.fmt("field %s must be exported", f.Name)
		}

		if f.Type.Kind() != reflect.Interface {
			return errInvalidType.fmt("field %s must be an interface, got %s", f.Name, f.Type.String())
		}
	}

	return nil
}

func toError(v reflect.Value) error {
	if v.IsNil() {
		return nil
	}

	return v.Interface().(error)
}
