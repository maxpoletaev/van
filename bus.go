package gobus

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

type provider interface{} // func(deps ...interface{}) interface{}
type handler interface{}  // func(ctx context.Context, cmd interface{}, deps ...interface{}) error

type emptyHandler func(ctx context.Context, cmd interface{}) error

type Bus interface {
	Provide(providerFn interface{})
	Handle(cmd interface{}, handlerFunc handler)
	Invoke(ctx context.Context, cmd interface{}) error
	Subscribe(event interface{}, listeners ...handler)
	Emit(ctx context.Context, event interface{}) (chan struct{}, chan error)
	Exec(fn interface{}) error
}

type busImpl struct {
	providers map[reflect.Type]provider
	handlers  map[reflect.Type]handler
	listeners map[reflect.Type][]handler
}

func New() Bus {
	bus := &busImpl{}
	bus.providers = make(map[reflect.Type]provider)

	// register artificial provider for Bus type so that we can access it from handlers
	bus.providers[reflect.TypeOf((*Bus)(nil)).Elem()] = func() Bus {
		return bus
	}

	bus.handlers = make(map[reflect.Type]handler)
	bus.listeners = make(map[reflect.Type][]handler)
	return bus
}

func (b *busImpl) Provide(providerFn interface{}) {
	providerType := reflect.TypeOf(providerFn)
	switch {
	case providerType.Kind() != reflect.Func:
		panic("provider must be a function")
	case providerType.NumOut() != 1:
		panic("provider must have one retunrn value")
	}

	retType := providerType.Out(0)
	switch {
	case retType.Kind() != reflect.Interface:
		panic("provider's return value must be an interface")
	}

	for i := 0; i < providerType.NumIn(); i++ {
		argType := providerType.In(i)
		if argType.Kind() != reflect.Interface {
			panic(fmt.Sprintf("provider's argument %d must be an interface", i))
		}
		if _, ok := b.providers[argType]; !ok {
			panic("no providers registered for type " + argType.String())
		}
	}

	b.providers[retType] = providerFn
}

func (b *busImpl) Handle(msg interface{}, handlerFunc handler) {
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
		panic("handler must have at least three arguments")
	case !isTypeContext(handlerT.In(0)):
		panic("handler's first argument must be a context")
	case !isTypeStructPointer(handlerT.In(1)):
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

	if _, ok := b.handlers[msgType]; !ok {
		b.handlers[msgType] = make([]handler, 0)
	}

	b.handlers[msgType] = handlerFunc
}

func (b *busImpl) Subscribe(event interface{}, handlerFuncs ...handler) {
	eventT := reflect.TypeOf(event)
	if eventT.Kind() != reflect.Struct {
		panic("event must be a struct")
	}

	if _, ok := b.listeners[eventT]; !ok {
		b.listeners[eventT] = make([]handler, 0)
	}

	b.listeners[eventT] = append(b.listeners[eventT], handlerFuncs...)
}

func (b *busImpl) Exec(fn interface{}) error {
	funcType := reflect.TypeOf(fn)
	switch {
	case funcType.NumOut() != 1:
		return fmt.Errorf("must have one return value")
	case !isTypeError(funcType.Out(0)):
		return fmt.Errorf("return value must be an error")
	}

	numIn := funcType.NumIn()
	if numIn == 0 {
		// Avoid expensive reflect.Value.Call() if handler has no dependencies
		return fn.(func() error)()
	}

	args := make([]reflect.Value, numIn)
	funcValue := reflect.ValueOf(fn)
	b.resolve(funcValue, args)
	ret := funcValue.Call(args)
	return toError(ret[0])
}

func (b *busImpl) Invoke(ctx context.Context, cmd interface{}) error {
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
	numIn := handlerV.Type().NumIn()
	if numIn == 2 {
		// Avoid expensive reflect.Value.Call() if handler has no dependencies
		return handler.(emptyHandler)(ctx, cmd)
	}

	args := make([]reflect.Value, numIn)
	args[0] = reflect.ValueOf(ctx)
	args[1] = reflect.ValueOf(cmd)

	err := b.resolve(handlerV, args)
	if err != nil {
		return err
	}

	ret := handlerV.Call(args)
	return toError(ret[0])
}

func (b *busImpl) Emit(ctx context.Context, event interface{}) (done chan struct{}, errchan chan error) {
	eventT := reflect.TypeOf(event)

	done = make(chan struct{})
	errchan = make(chan error, 1)
	if eventT.Kind() != reflect.Ptr {
		errchan <- fmt.Errorf("event must be a pointer to a struct")
		close(done)
		return
	}

	eventT = eventT.Elem()
	if eventT.Kind() != reflect.Struct {
		errchan <- fmt.Errorf("event must be a pointer to a struct")
		close(done)
		return
	}

	listeners, ok := b.listeners[eventT]
	if !ok {
		close(done)
		return
	}

	wg := sync.WaitGroup{}
	errchan = make(chan error, len(listeners))
	for _, listener := range listeners {
		listenerV := reflect.ValueOf(listener)
		args := make([]reflect.Value, listenerV.Type().NumIn())
		args[0] = reflect.ValueOf(ctx)
		args[1] = reflect.ValueOf(event)

		err := b.resolve(listenerV, args)
		if err != nil {
			errchan <- err
			continue
		}

		wg.Add(1)
		go func(f reflect.Value, args []reflect.Value) {
			defer wg.Done()
			ret := f.Call(args)
			if err := toError(ret[0]); err != nil {
				errchan <- err
			}
		}(listenerV, args)
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	return
}

func (b *busImpl) resolve(funcV reflect.Value, args []reflect.Value) error {
	funcT := funcV.Type()
	for i := 0; i < funcT.NumIn(); i++ {
		if args[i].IsValid() {
			if _, ok := args[i].Interface().(context.Context); ok {
				continue
			}
		}

		argType := funcT.In(i)
		if argType.Kind() != reflect.Interface {
			continue
		}

		providerFunc := b.providers[argType]
		providerV := reflect.ValueOf(providerFunc)
		nextArgs := make([]reflect.Value, providerV.Type().NumIn())
		err := b.resolve(providerV, nextArgs)
		if err != nil {
			return err
		}

		args[i] = providerV.Call(nextArgs)[0]
	}
	return nil
}

func isTypeStructPointer(t reflect.Type) bool {
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct
}

func isTypeContext(t reflect.Type) bool {
	return t.Implements(reflect.TypeOf((*context.Context)(nil)).Elem())
}

func isTypeError(t reflect.Type) bool {
	return t.Implements(reflect.TypeOf((*error)(nil)).Elem())
}

func toError(v reflect.Value) error {
	if v.IsNil() {
		return nil
	}
	return v.Interface().(error)
}
