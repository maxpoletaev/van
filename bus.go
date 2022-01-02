package gobus

import (
	"context"
	"fmt"
	"reflect"
)

type provider interface{} // func(deps ...interface{}) interface{}
type handler interface{}  // func(ctx context.Context, cmd interface{}, deps ...interface{}) error

type Bus struct {
	providers map[reflect.Type]provider
	handlers  map[reflect.Type]handler
}

func New() *Bus {
	bus := &Bus{}
	bus.handlers = make(map[reflect.Type]handler)
	bus.providers = make(map[reflect.Type]provider)
	return bus
}

func (b *Bus) Provide(providerFunc interface{}) {
	providerFuncType := reflect.TypeOf(providerFunc)
	switch {
	case providerFuncType.Kind() != reflect.Func:
		panic("provider must be a function")
	case providerFuncType.NumOut() != 1:
		panic("provider must have one retunrn value")
	}

	retType := providerFuncType.Out(0)
	switch {
	case retType.Kind() != reflect.Interface:
		panic("provider's return value must be an interface")
	}

	b.providers[retType] = providerFunc
}

func (b *Bus) Handle(msg interface{}, handlerFunc handler) {
	msgType := reflect.TypeOf(msg)
	switch {
	case msgType.Kind() != reflect.Struct:
		panic("msg must be a struct")
	}

	if _, ok := b.handlers[msgType]; ok {
		panic("handler already registered for type: " + msgType.String())
	}

	handlerT := reflect.TypeOf(handlerFunc)
	switch {
	case handlerT.Kind() != reflect.Func:
		panic("handler must be a function")
	case handlerT.NumIn() < 2:
		panic("handler must have at least two arguments")
	case !handlerT.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()):
		panic("handler's first argument must be a context")
	case handlerT.In(1).Kind() != reflect.Ptr && handlerT.In(1).Elem().Kind() != reflect.Struct:
		panic("handler's second argument must be a pointer to a struct")
	}

	for i := 2; i < handlerT.NumIn(); i++ {
		argType := handlerT.In(i)
		if argType.Kind() != reflect.Interface {
			panic(fmt.Sprintf("handler's argument %d must be an interface", i))
		}
		if _, ok := b.providers[argType]; !ok {
			panic("there's no provider found for type: " + argType.String())
		}
	}

	b.handlers[msgType] = handlerFunc
}

func (b *Bus) Dispatch(ctx context.Context, cmd interface{}) error {
	cmdT := reflect.TypeOf(cmd)
	if cmdT.Kind() != reflect.Ptr {
		return fmt.Errorf("cmd must be a pointer to a struct")
	}

	cmdT = cmdT.Elem()
	if cmdT.Kind() != reflect.Struct {
		return fmt.Errorf("cmd must be a pointer to a struct")
	}

	handler, ok := b.handlers[cmdT]
	if !ok {
		return fmt.Errorf("no handlers found for type: %s", cmdT.String())
	}

	handlerV := reflect.ValueOf(handler)
	args := make([]reflect.Value, handlerV.Type().NumIn())
	args[0] = reflect.ValueOf(ctx)
	args[1] = reflect.ValueOf(cmd)

	seen := make(map[uintptr]struct{})
	err := b.resolveRecursive(handlerV, seen, args)
	if err != nil {
		return err
	}

	res := handlerV.Call(args)
	if errVal := res[0]; !errVal.IsNil() {
		return errVal.Interface().(error)
	}

	return nil
}

func (b *Bus) resolveRecursive(funcV reflect.Value, seen map[uintptr]struct{}, args []reflect.Value) error {
	funcKey := funcV.Pointer()
	if _, ok := seen[funcKey]; ok {
		panic(fmt.Errorf("circular dependency detected: %s", funcV.Type().String()))
	}

	nextSeen := make(map[uintptr]struct{}, len(seen))
	for k := range seen {
		nextSeen[k] = seen[k]
	}
	nextSeen[funcKey] = struct{}{}

	funcT := funcV.Type()
	for i := 0; i < funcT.NumIn(); i++ {
		argType := funcT.In(i)
		if argType.Kind() == reflect.Interface {
			if providerFunc, ok := b.providers[argType]; ok {
				providerV := reflect.ValueOf(providerFunc)
				nextArgs := make([]reflect.Value, providerV.Type().NumIn())
				err := b.resolveRecursive(providerV, nextSeen, nextArgs)
				if err != nil {
					return err
				}

				args[i] = providerV.Call(nextArgs)[0]
			}
		}
	}

	return nil
}
