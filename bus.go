package van

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

type providerFunc interface{} // func(deps ...interface{}) interface{}
type handlerFunc interface{}  // func(ctx context.Context, cmd interface{}, deps ...interface{}) error

type Bus interface {
	Provide(provider providerFunc)
	HandleCommand(cmd interface{}, handler handlerFunc)
	InvokeCommand(ctx context.Context, cmd interface{}) error
	ListenEvent(event interface{}, listeners ...handlerFunc)
	EmitEvent(ctx context.Context, event interface{}) (chan struct{}, chan error)
	Exec(fn interface{}) error
}

type busImpl struct {
	providers map[reflect.Type]providerFunc
	handlers  map[reflect.Type]handlerFunc
	listeners map[reflect.Type][]handlerFunc
}

func New() Bus {
	b := &busImpl{}
	b.providers = make(map[reflect.Type]providerFunc)

	// register provider for Bus type so that we can access it from handlers
	b.providers[reflect.TypeOf((*Bus)(nil)).Elem()] = func() Bus {
		return b
	}

	b.handlers = make(map[reflect.Type]handlerFunc)
	b.listeners = make(map[reflect.Type][]handlerFunc)
	return b
}

func (b *busImpl) Provide(provider providerFunc) {
	providerType := reflect.TypeOf(provider)
	switch {
	case providerType.Kind() != reflect.Func:
		panic("provider must be a function")
	case providerType.NumOut() != 1:
		panic("provider must have one return value")
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

	b.providers[retType] = provider
}

func (b *busImpl) HandleCommand(cmd interface{}, handler handlerFunc) {
	cmdType := reflect.TypeOf(cmd)
	switch {
	case cmdType.Kind() != reflect.Struct:
		panic("msg must be a struct")
	}

	if _, ok := b.handlers[cmdType]; ok {
		panic("handler already registered for type " + cmdType.String())
	}

	handlerType := reflect.TypeOf(handler)
	b.validateHandler(handlerType)

	for i := 2; i < handlerType.NumIn(); i++ {
		argType := handlerType.In(i)
		if argType.Kind() != reflect.Interface {
			panic(fmt.Sprintf("handler's argument %d must be an interface", i))
		}
		if _, ok := b.providers[argType]; !ok {
			panic("no providers registered for type " + argType.String())
		}
	}

	b.handlers[cmdType] = handler
}

func (b *busImpl) InvokeCommand(ctx context.Context, cmd interface{}) error {
	cmdType := reflect.TypeOf(cmd)
	if cmdType.Kind() != reflect.Ptr {
		return fmt.Errorf("cmd must be a pointer to a struct")
	}
	cmdType = cmdType.Elem()
	if cmdType.Kind() != reflect.Struct {
		return fmt.Errorf("cmd must be a pointer to a struct")
	}

	handler, ok := b.handlers[cmdType]
	if !ok {
		return fmt.Errorf("no handlers found for type %s", cmdType.String())
	}

	handlerValue := reflect.ValueOf(handler)
	args := make([]reflect.Value, handlerValue.Type().NumIn())
	args[0] = reflect.ValueOf(ctx)
	args[1] = reflect.ValueOf(cmd)

	err := b.resolve(handlerValue, args)
	if err != nil {
		return err
	}

	ret := handlerValue.Call(args)
	return toError(ret[0])
}

func (b *busImpl) ListenEvent(event interface{}, listeners ...handlerFunc) {
	eventType := reflect.TypeOf(event)
	if eventType.Kind() != reflect.Struct {
		panic("event must be a struct")
	}

	for i := range listeners {
		listenerType := reflect.TypeOf(listeners[i])
		b.validateListener(listenerType)
		if eventType != listenerType.In(1) {
			panic("event type mismatch")
		}
	}

	if _, ok := b.listeners[eventType]; !ok {
		b.listeners[eventType] = make([]handlerFunc, 0)
	}

	b.listeners[eventType] = append(b.listeners[eventType], listeners...)
}

func (b *busImpl) EmitEvent(ctx context.Context, event interface{}) (done chan struct{}, errchan chan error) {
	done = make(chan struct{})
	errchan = make(chan error, 1)

	eventType := reflect.TypeOf(event)
	if eventType.Kind() != reflect.Struct {
		errchan <- fmt.Errorf("event must be a a struct")
		close(done)
		close(errchan)
		return
	}

	listeners, ok := b.listeners[eventType]
	if !ok {
		close(done)
		close(errchan)
		return
	}

	wg := &sync.WaitGroup{}
	errchan = make(chan error, len(listeners))
	for _, listener := range listeners {
		listenerValue := reflect.ValueOf(listener)
		args := make([]reflect.Value, listenerValue.Type().NumIn())
		args[0] = reflect.ValueOf(ctx)
		args[1] = reflect.ValueOf(event)

		err := b.resolve(listenerValue, args)
		if err != nil {
			errchan <- err
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			ret := listenerValue.Call(args)
			if err := toError(ret[0]); err != nil {
				errchan <- err
			}
		}()
	}

	go func() {
		wg.Wait()
		close(done)
		close(errchan)
	}()

	return
}

func (b *busImpl) Exec(fn interface{}) error {
	funcType := reflect.TypeOf(fn)
	switch {
	case funcType.NumOut() != 1:
		return fmt.Errorf("must have one return value")
	case !isTypeError(funcType.Out(0)):
		return fmt.Errorf("return value must be an error")
	}

	for i := 0; i <= funcType.NumIn(); i++ {
		argType := funcType.In(i)
		if argType.Kind() != reflect.Interface {
			return fmt.Errorf("function argument %d is not an interface", i)
		}
	}

	args := make([]reflect.Value, funcType.NumIn())
	funcValue := reflect.ValueOf(fn)
	b.resolve(funcValue, args)
	ret := funcValue.Call(args)
	return toError(ret[0])
}

func (b *busImpl) resolve(funcValue reflect.Value, args []reflect.Value) error {
	funcT := funcValue.Type()
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

		provider := b.providers[argType]
		providerType := reflect.TypeOf(provider)
		providerValue := reflect.ValueOf(provider)

		nextArgs := make([]reflect.Value, providerType.NumIn())
		err := b.resolve(providerValue, nextArgs)
		if err != nil {
			return err
		}

		args[i] = providerValue.Call(nextArgs)[0]
	}
	return nil
}

func (b *busImpl) validateHandler(handlerType reflect.Type) {
	switch {
	case handlerType.Kind() != reflect.Func:
		panic("handler must be a function")
	case handlerType.NumIn() < 2:
		panic("handler must have at least 2 arguments")
	case !isTypeContext(handlerType.In(0)):
		panic("handler's first argument must be the context")
	case !isTypeStructPointer(handlerType.In(1)):
		panic("handler's second argument must be a struct pointer")
	}

	// start from the third argument as the first two are always `ctx` and `cmd`
	for i := 2; i < handlerType.NumIn(); i++ {
		argType := handlerType.In(i)
		if argType.Kind() != reflect.Interface {
			panic(fmt.Sprintf("handler's argument %d must be an interface", i))
		}
		if _, ok := b.providers[argType]; !ok {
			panic("no providers registered for type " + argType.String())
		}
	}
}

func (b *busImpl) validateListener(listenerType reflect.Type) {
	switch {
	case listenerType.Kind() != reflect.Func:
		panic("listener must be a function")
	case listenerType.NumIn() < 2:
		panic("listener must have at least 2 arguments")
	case !isTypeContext(listenerType.In(0)):
		panic("listener's first argument must be the context")
	case listenerType.In(1).Kind() != reflect.Struct:
		panic("listener's second argument must be a struct")
	}

	// start from the third argument as the first two are always `ctx` and `event`
	for i := 2; i < listenerType.NumIn(); i++ {
		argType := listenerType.In(i)
		if argType.Kind() != reflect.Interface {
			panic(fmt.Sprintf("listener's argument %d must be an interface", i))
		}
		if _, ok := b.providers[argType]; !ok {
			panic("no providers registered for type " + argType.String())
		}
	}
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
