package van

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

type ProviderFunc interface{} // func(ctx context.Context, deps ...interface{}) (interface{}, error)
type HandlerFunc interface{}  // func(ctx context.Context, cmd interface{}, deps ...interface{}) error
type ListenerFunc interface{} // func(ctx context.Context, event interface{}, deps ...interface)

type providerOpts struct {
	fn        ProviderFunc
	singleton bool
}

type Van interface {
	// Provide registers new type constructor that will be called every time a handler requests the dependency.
	// There's no such thing as "optional" dependency. Therefore the provider should either return a valid non-nil
	// dependency or an error.
	// It is expected to be called during the app startup phase as it performs the run time type checking and
	// PANICS if an incorrect function type is provided.
	Provide(provider ProviderFunc)

	// ProvideSingleton registers a new type constructor that is guaranteed to be called not more than once in
	// application's lifetime.
	// It is expected to be called during the app startup phase as it performs the run time type checking and
	// PANICS if an incorrect function type is provided.
	ProvideSingleton(provider ProviderFunc)

	// Handle registers a handler for the given command type. There can be only one handler per command.
	// It is expected to be called during the app startup phase as it performs the run time type checking and
	// PANICS if an incorrect function type is provided.
	Handle(cmd interface{}, handler HandlerFunc)

	// Subscribe registers a new handler for the given command type. There can be any number of handlers per event.
	// It is expected to be called during the app startup phase as it performs the run time type checking and
	// PANICS if an incorrect function type is provided.
	Subscribe(event interface{}, listeners ...ListenerFunc)

	// Invoke runs an associated command handler.
	Invoke(ctx context.Context, cmd interface{}) error

	// Publish sends an event to the bus. Listeners are executed concurrently and can fail independently.
	// Only the first error is returned, even though there might be more than one failing listener.
	Publish(ctx context.Context, event interface{}) error

	// Exec executes the given function inside the dependency injector.
	Exec(ctx context.Context, fn interface{}) error

	// Wait blocks until all current events are processed. Useful for graceful shutdown. The app should
	// ensure no new events/commands are published. Otherwise, it may run forever.
	Wait()
}

type busImpl struct {
	providers   map[reflect.Type]providerOpts
	handlers    map[reflect.Type]HandlerFunc
	listeners   map[reflect.Type][]HandlerFunc
	instances   map[reflect.Type]interface{}
	instancesMu sync.RWMutex
	runnig      sync.WaitGroup
}

func New() Van {
	b := &busImpl{}
	b.providers = make(map[reflect.Type]providerOpts)
	b.handlers = make(map[reflect.Type]HandlerFunc)
	b.listeners = make(map[reflect.Type][]HandlerFunc)
	b.instances = make(map[reflect.Type]interface{})
	b.runnig = sync.WaitGroup{}
	return b
}

func (b *busImpl) Wait() {
	b.runnig.Wait()
}

func (b *busImpl) Provide(provider ProviderFunc) {
	providerType := reflect.TypeOf(provider)
	if err := b.validateProviderType(providerType); err != nil {
		panic(err)
	}

	retType := providerType.Out(0)
	b.providers[retType] = providerOpts{fn: provider}
}

func (b *busImpl) ProvideSingleton(provider ProviderFunc) {
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

func (b *busImpl) Handle(cmd interface{}, handler HandlerFunc) {
	err := b.registerHandler(cmd, handler)
	if err != nil {
		panic(err)
	}
}

func (b *busImpl) registerHandler(cmd interface{}, handler HandlerFunc) error {
	cmdType := reflect.TypeOf(cmd)
	if cmdType.Kind() != reflect.Struct {
		return errInvalidType.fmt("cmd must be a struct, got %s", cmdType.Name())
	}

	handlerType := reflect.TypeOf(handler)
	if err := b.validateHandlerType(handlerType); err != nil {
		return err
	}

	if cmdType != handlerType.In(1).Elem() {
		return errInvalidType.new("command type mismatch")
	}

	b.handlers[cmdType] = handler
	return nil
}

func (b *busImpl) Invoke(ctx context.Context, cmd interface{}) error {
	cmdType := reflect.TypeOf(cmd)
	if cmdType.Kind() != reflect.Ptr {
		return errInvalidType.new("cmd must be a pointer to a struct")
	}
	cmdType = cmdType.Elem()
	if cmdType.Kind() != reflect.Struct {
		return errInvalidType.new("cmd must be a pointer to a struct")
	}

	handler, ok := b.handlers[cmdType]
	if !ok {
		return errProviderNotFound.fmt("no handlers found for type %s", cmdType.String())
	}

	handlerType := reflect.TypeOf(handler)
	args := make([]reflect.Value, handlerType.NumIn())
	err := b.resolve(ctx, cmd, handlerType, args)
	if err != nil {
		return err
	}

	b.runnig.Add(1)
	defer b.runnig.Done()

	ret := reflect.ValueOf(handler).Call(args)
	return toError(ret[0])
}

func (b *busImpl) Subscribe(event interface{}, listeners ...ListenerFunc) {
	for i := range listeners {
		err := b.registerListener(event, listeners[i])
		if err != nil {
			panic(err)
		}
	}
}

func (b *busImpl) registerListener(event interface{}, listener ListenerFunc) error {
	eventType := reflect.TypeOf(event)
	if eventType.Kind() != reflect.Struct {
		return errInvalidType.fmt("event must be a struct, got %s", eventType.String())
	}

	listenerType := reflect.TypeOf(listener)
	if err := b.validateListener(listenerType); err != nil {
		return err
	}

	if eventType != listenerType.In(1) {
		return errInvalidType.new("event type mismatch")
	}

	if _, ok := b.listeners[eventType]; !ok {
		b.listeners[eventType] = make([]HandlerFunc, 0)
	}

	b.listeners[eventType] = append(b.listeners[eventType], listener)
	return nil
}

func (b *busImpl) Publish(ctx context.Context, event interface{}) error {
	eventType := reflect.TypeOf(event)
	if eventType.Kind() != reflect.Struct {
		return errInvalidType.fmt("event must be a a struct, got %s", eventType.Name())
	}

	listeners, ok := b.listeners[eventType]
	if !ok {
		return nil
	}

	largs := make([][]reflect.Value, len(listeners))
	for i := range listeners {
		listenerType := reflect.TypeOf(listeners[i])
		if numIn := listenerType.NumIn(); numIn > 0 {
			largs[i] = make([]reflect.Value, numIn)
			err := b.resolve(ctx, event, listenerType, largs[i])
			if err != nil {
				return err
			}
		}
	}

	wg := sync.WaitGroup{}
	for i := range listeners {
		wg.Add(1)
		b.runnig.Add(1)
		go func(i int) {
			defer wg.Done()
			defer b.runnig.Done()
			reflect.ValueOf(listeners[i]).Call(largs[i])
		}(i)
	}

	wg.Wait()
	return nil
}

func (b *busImpl) Exec(ctx context.Context, fn interface{}) error {
	funcType := reflect.TypeOf(fn)
	switch {
	case funcType.Kind() != reflect.Func:
		return errInvalidType.fmt("fn should be a function, got %s", funcType.String())
	case funcType.NumOut() != 1:
		return errInvalidType.fmt("fn must have one return value, got %s", fmt.Sprint(funcType.NumOut()))
	case !funcType.Out(0).Implements(typeError):
		return errInvalidType.fmt("return value must be an error, got %s", funcType.Out(0).String())
	}

	for i := 0; i < funcType.NumIn(); i++ {
		argType := funcType.In(i)
		if argType.Kind() != reflect.Interface {
			return errInvalidType.fmt("function argument %d is not an interface", i)
		}
		if !b.hasProvider(argType) {
			return errProviderNotFound.fmt("no providers registered for type %s", argType.String())
		}
	}

	args := make([]reflect.Value, funcType.NumIn())
	err := b.resolve(ctx, nil, funcType, args)
	if err != nil {
		return err
	}

	ret := reflect.ValueOf(fn).Call(args)
	return toError(ret[0])
}

func (b *busImpl) resolve(ctx context.Context, cmd interface{}, funcType reflect.Type, args []reflect.Value) error {
	for i := 0; i < funcType.NumIn(); i++ {
		argType := funcType.In(i)
		switch {
		case i == 0 && argType == typeContext:
			args[i] = reflect.ValueOf(ctx)
		case i == 1 && argType == reflect.TypeOf(cmd):
			args[i] = reflect.ValueOf(cmd)
		case argType == typeVan:
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
		if inst, ok := b.instances[t]; ok {
			b.instancesMu.RUnlock()
			return reflect.ValueOf(inst), nil
		}
		b.instancesMu.RUnlock()
	}

	var args []reflect.Value
	providerType := reflect.TypeOf(provider.fn)
	if numIn := providerType.NumIn(); numIn > 0 {
		args = make([]reflect.Value, numIn)
		err := b.resolve(ctx, nil, providerType, args)
		if err != nil {
			return reflect.ValueOf(nil), err
		}
	}

	providerValue := reflect.ValueOf(provider.fn)
	if provider.singleton {
		return func() (reflect.Value, error) {
			b.instancesMu.Lock()
			defer b.instancesMu.Unlock()

			if inst, ok := b.instances[t]; ok {
				return reflect.ValueOf(inst), nil
			}

			ret := providerValue.Call(args)
			instValue, err := ret[0], toError(ret[1])
			if err != nil {
				return reflect.ValueOf(nil), fmt.Errorf("failed to resolve dependency %s: %w", t.String(), err)
			}

			b.instances[t] = instValue.Interface()
			return instValue, nil
		}()
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
		return errInvalidType.fmt("provider must be a function, got %s", t.String())
	case t.NumOut() != 2:
		return errInvalidType.fmt("provider must have two return values, got %d", t.NumOut())
	case t.Out(0).Kind() != reflect.Interface:
		return errInvalidType.fmt("provider's first return value must be an interface, got %s", t.Out(0).String())
	case !t.Out(1).Implements(typeError):
		return errInvalidType.fmt("provider's second return value must be an error, got %s", t.Out(1).String())
	}

	for i := 0; i < t.NumIn(); i++ {
		argType := t.In(i)
		if argType.Kind() != reflect.Interface {
			return errInvalidType.fmt("provider's argument %d must be an interface, got %s", i, argType.String())
		}
		if !b.hasProvider(argType) {
			return errProviderNotFound.fmt("no providers registered for type %s", argType.String())
		}
	}

	return nil
}

func (b *busImpl) validateHandlerType(t reflect.Type) error {
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

	// start from the third argument as the first two are always `ctx` and `cmd`
	for i := 2; i < t.NumIn(); i++ {
		argType := t.In(i)
		if argType.Kind() != reflect.Interface {
			return errInvalidType.fmt("handler's argument %d must be an interface, got %s", i, argType.String())
		}
		if !b.hasProvider(argType) {
			return errProviderNotFound.fmt("no providers registered for type %s", argType.String())
		}
	}

	return nil
}

func (b *busImpl) validateListener(t reflect.Type) error {
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

	// start from the third argument as the first two are always `ctx` and `cmd`
	for i := 2; i < t.NumIn(); i++ {
		argType := t.In(i)
		if argType.Kind() != reflect.Interface {
			return errInvalidType.fmt("handler's argument %d must be an interface, got %s", i, argType.String())
		}
		if !b.hasProvider(argType) {
			return errProviderNotFound.fmt("no providers registered for type %s", argType.String())
		}
	}

	return nil
}

func (b *busImpl) hasProvider(t reflect.Type) bool {
	if _, ok := b.providers[t]; ok || t == typeVan || t == typeContext {
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
