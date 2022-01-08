package van

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

type providerFunc interface{} // func(ctx context.Context, deps ...interface{}) (interface{}, error)
type handlerFunc interface{}  // func(ctx context.Context, cmd interface{}, deps ...interface{}) error
type listenerFunc interface{} // func(ctx context.Context, event interface{}, deps ...interface) error

type providerOpts struct {
	fn        providerFunc
	singleton bool
}

type Van interface {
	Provide(provider providerFunc)
	ProvideSingleton(provider providerFunc)
	Handle(cmd interface{}, handler handlerFunc)
	Invoke(ctx context.Context, cmd interface{}) error
	Subscribe(event interface{}, listeners ...listenerFunc)
	Publish(ctx context.Context, event interface{}) (chan struct{}, chan error)
	Exec(ctx context.Context, fn interface{}) error
}

type busImpl struct {
	providers   map[reflect.Type]providerOpts
	handlers    map[reflect.Type]handlerFunc
	listeners   map[reflect.Type][]handlerFunc
	instances   map[reflect.Type]interface{}
	instancesMu sync.RWMutex
}

func New() Van {
	b := &busImpl{}
	b.providers = make(map[reflect.Type]providerOpts)
	b.handlers = make(map[reflect.Type]handlerFunc)
	b.listeners = make(map[reflect.Type][]handlerFunc)
	b.instances = make(map[reflect.Type]interface{})
	return b
}

func (b *busImpl) Provide(provider providerFunc) {
	providerType := reflect.TypeOf(provider)
	if err := b.validateProviderType(providerType); err != nil {
		panic(err)
	}

	retType := providerType.Out(0)
	b.providers[retType] = providerOpts{fn: provider}
}

func (b *busImpl) ProvideSingleton(provider providerFunc) {
	providerType := reflect.TypeOf(provider)
	if err := b.validateProviderType(providerType); err != nil {
		panic(err)
	}

	retType := providerType.Out(0)
	b.providers[retType] = providerOpts{
		fn:        provider,
		singleton: true,
	}
}

func (b *busImpl) Handle(cmd interface{}, handler handlerFunc) {
	err := b.registerHandler(cmd, handler)
	if err != nil {
		panic(err)
	}
}

func (b *busImpl) registerHandler(cmd interface{}, handler handlerFunc) error {
	cmdType := reflect.TypeOf(cmd)
	if cmdType.Kind() != reflect.Struct {
		return ErrInvalidType.new("msg must be a struct, got " + cmdType.Name())
	}

	handlerType := reflect.TypeOf(handler)
	if err := b.validateHandlerType(handlerType); err != nil {
		return err
	}

	if cmdType != handlerType.In(1).Elem() {
		return ErrInvalidType.new("command type mismatch")
	}

	b.handlers[cmdType] = handler
	return nil
}

func (b *busImpl) Invoke(ctx context.Context, cmd interface{}) error {
	cmdType := reflect.TypeOf(cmd)
	if cmdType.Kind() != reflect.Ptr {
		return ErrInvalidType.new("cmd must be a pointer to a struct")
	}
	cmdType = cmdType.Elem()
	if cmdType.Kind() != reflect.Struct {
		return ErrInvalidType.new("cmd must be a pointer to a struct")
	}

	handler, ok := b.handlers[cmdType]
	if !ok {
		return ErrProviderNotFound.new("no handlers found for type " + cmdType.String())
	}

	handlerValue := reflect.ValueOf(handler)
	args := make([]reflect.Value, handlerValue.Type().NumIn())
	err := b.resolve(ctx, cmd, handlerValue, args)
	if err != nil {
		return err
	}

	ret := handlerValue.Call(args)
	return toError(ret[0])
}

func (b *busImpl) Subscribe(event interface{}, listeners ...listenerFunc) {
	for i := range listeners {
		err := b.registerListener(event, listeners[i])
		if err != nil {
			panic(err)
		}
	}
}

func (b *busImpl) registerListener(event interface{}, listener listenerFunc) error {
	eventType := reflect.TypeOf(event)
	if eventType.Kind() != reflect.Struct {
		return ErrInvalidType.new("event must be a struct, got " + eventType.String())
	}

	listenerType := reflect.TypeOf(listener)
	if err := b.validateListener(listenerType); err != nil {
		return err
	}

	if eventType != listenerType.In(1) {
		return ErrInvalidType.new("event type mismatch")
	}

	if _, ok := b.listeners[eventType]; !ok {
		b.listeners[eventType] = make([]handlerFunc, 0)
	}

	b.listeners[eventType] = append(b.listeners[eventType], listener)
	return nil
}

func (b *busImpl) Publish(ctx context.Context, event interface{}) (done chan struct{}, errchan chan error) {
	done = make(chan struct{})
	errchan = make(chan error, 1)

	eventType := reflect.TypeOf(event)
	if eventType.Kind() != reflect.Struct {
		errchan <- ErrInvalidType.new("event must be a a struct, got " + eventType.Name())
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
		err := b.resolve(ctx, event, listenerValue, args)
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

func (b *busImpl) Exec(ctx context.Context, fn interface{}) error {
	funcType := reflect.TypeOf(fn)
	switch {
	case funcType.Kind() != reflect.Func:
		return ErrInvalidType.new("fn should be a function, got " + funcType.String())
	case funcType.NumOut() != 1:
		return ErrInvalidType.new("fn must have one return value, got " + fmt.Sprint(funcType.NumOut()))
	case !isError(funcType.Out(0)):
		return ErrInvalidType.new("return value must be an error, got" + funcType.Out(0).String())
	}

	for i := 0; i < funcType.NumIn(); i++ {
		argType := funcType.In(i)
		if argType.Kind() != reflect.Interface {
			return fmt.Errorf("function argument %d is not an interface", i)
		}
		if !b.hasProvider(argType) {
			return ErrProviderNotFound.new("no providers registered for type " + argType.String())
		}
	}

	funcValue := reflect.ValueOf(fn)
	args := make([]reflect.Value, funcType.NumIn())
	err := b.resolve(ctx, nil, funcValue, args)
	if err != nil {
		return err
	}

	ret := funcValue.Call(args)
	return toError(ret[0])
}

func (b *busImpl) resolve(
	ctx context.Context,
	cmd interface{},
	funcValue reflect.Value,
	args []reflect.Value,
) error {
	funcType := funcValue.Type()
	for i := 0; i < funcType.NumIn(); i++ {
		argType := funcType.In(i)
		switch {
		case i == 0 && isContext(argType):
			args[i] = reflect.ValueOf(ctx)
		case i == 1 && argType == reflect.TypeOf(cmd):
			args[i] = reflect.ValueOf(cmd)
		case isBusItself(argType):
			args[i] = reflect.ValueOf(b)
		case argType.Kind() == reflect.Interface:
			instance, err := b.new(ctx, argType)
			if err != nil {
				return err
			}
			args[i] = instance
		}
	}
	return nil
}

func (b *busImpl) new(ctx context.Context, t reflect.Type) (reflect.Value, error) {
	provider := b.providers[t]

	if provider.singleton {
		b.instancesMu.RLock()
		if instance, ok := b.instances[t]; ok {
			b.instancesMu.RUnlock()
			return reflect.ValueOf(instance), nil
		}
		b.instancesMu.RUnlock()
	}

	providerType := reflect.TypeOf(provider.fn)
	providerValue := reflect.ValueOf(provider.fn)

	numIn := providerType.NumIn()
	var args []reflect.Value
	if numIn > 0 {
		args = make([]reflect.Value, numIn)
	}

	err := b.resolve(ctx, nil, providerValue, args)
	if err != nil {
		return reflect.ValueOf(nil), err
	}

	if provider.singleton {
		b.instancesMu.Lock()
		if instance, ok := b.instances[t]; ok {
			b.instancesMu.Unlock()
			return reflect.ValueOf(instance), nil
		}

		ret := providerValue.Call(args)
		instanceValue, err := ret[0], toError(ret[1])
		if err != nil {
			b.instancesMu.Unlock()
			err = fmt.Errorf("failed to resolve dependency %s: %w", t.String(), err)
			return reflect.ValueOf(nil), err
		}

		b.instances[t] = instanceValue.Interface()
		b.instancesMu.Unlock()

		return instanceValue, nil
	}

	ret := providerValue.Call(args)
	if err := toError(ret[1]); err != nil {
		return reflect.ValueOf(nil), fmt.Errorf("failed to resolve dependency %s: %w", t.String(), err)
	}

	return ret[0], nil
}

func (b *busImpl) validateProviderType(t reflect.Type) error {
	switch {
	case t.Kind() != reflect.Func:
		return ErrInvalidType.new("provider must be a function, got " + t.String())
	case t.NumOut() != 2:
		return ErrInvalidType.new("provider must have two return values, got " + fmt.Sprint(t.NumOut()))
	case t.Out(0).Kind() != reflect.Interface:
		return ErrInvalidType.new("provider's first return value must be an interface, got " + t.Out(0).String())
	case !isError(t.Out(1)):
		return ErrInvalidType.new("provider's second return value must be an error, got " + t.Out(1).String())
	}

	for i := 0; i < t.NumIn(); i++ {
		argType := t.In(i)
		if argType.Kind() != reflect.Interface {
			return ErrInvalidType.new(fmt.Sprintf("provider's argument %d must be an interface, got %s", i, argType.String()))
		}
		if !b.hasProvider(argType) {
			return ErrProviderNotFound.new("no providers registered for type " + argType.String())
		}
	}

	return nil
}

func (b *busImpl) validateHandlerType(t reflect.Type) error {
	switch {
	case t.Kind() != reflect.Func:
		return ErrInvalidType.new("handler must be a function, got " + t.String())
	case t.NumIn() < 2:
		return ErrInvalidType.new("handler must have at least 2 arguments, got " + fmt.Sprint(t.NumIn()))
	case !isContext(t.In(0)):
		return ErrInvalidType.new("handler's first argument must be context.Context, got " + t.In(0).String())
	case !isStructPtr(t.In(1)):
		return ErrInvalidType.new("handler's second argument must be a struct pointer, got " + t.In(1).String())
	case t.NumOut() != 1:
		return ErrInvalidType.new("handler must have one return value, got " + fmt.Sprint(t.NumOut()))
	case !isError(t.Out(0)):
		return ErrInvalidType.new("handler's return type must be error, got " + t.Out(0).String())
	}

	// start from the third argument as the first two are always `ctx` and `cmd`
	for i := 2; i < t.NumIn(); i++ {
		argType := t.In(i)
		if argType.Kind() != reflect.Interface {
			return ErrInvalidType.new(fmt.Sprintf("handler's argument %d must be an interface, got %s", i, argType.String()))
		}
		if !b.hasProvider(argType) {
			return ErrProviderNotFound.new("no providers registered for type " + argType.String())
		}
	}

	return nil
}

func (b *busImpl) validateListener(t reflect.Type) error {
	switch {
	case t.Kind() != reflect.Func:
		return ErrInvalidType.new("handler must be a function, got " + t.String())
	case t.NumIn() < 2:
		return ErrInvalidType.new("handler must have at least 2 arguments, got " + fmt.Sprint(t.NumIn()))
	case !isContext(t.In(0)):
		return ErrInvalidType.new("handler's first argument must be context.Context, got " + t.In(0).String())
	case t.In(1).Kind() != reflect.Struct:
		return ErrInvalidType.new("handler's second argument must be a struct, got " + t.In(1).String())
	}

	// start from the third argument as the first two are always `ctx` and `cmd`
	for i := 2; i < t.NumIn(); i++ {
		argType := t.In(i)
		if argType.Kind() != reflect.Interface {
			return ErrInvalidType.new(fmt.Sprintf("handler's argument %d must be an interface, got %s", i, argType.String()))
		}
		if !b.hasProvider(argType) {
			return ErrProviderNotFound.new("no providers registered for type " + argType.String())
		}
	}

	return nil
}

func (b *busImpl) hasProvider(t reflect.Type) bool {
	_, ok := b.providers[t]
	if ok || isBusItself(t) || isContext(t) {
		return true
	}
	return false
}

func toError(v reflect.Value) error {
	if v.IsNil() {
		return nil
	}
	return v.Interface().(error)
}
