package van

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

// Maximum number of arguments to be allocated on the stack.
// Lower values will use the heap more aggressively which will slow down the program.
// Higher values will speed up the execution for functions with a large number of
// dependencies but may increase memory consumption as well.
const maxArgsOnStack = 16

type ProviderFunc interface{} // func(ctx context.Context, deps ...interface{}) (interface{}, error)
type HandlerFunc interface{}  // func(ctx context.Context, cmd interface{}, deps ...interface{}) error
type ListenerFunc interface{} // func(ctx context.Context, event interface{}, deps ...interface)

type providerOpts struct {
	sync.RWMutex

	fn        ProviderFunc
	instance  interface{}
	singleton bool
}

func (p *providerOpts) call(args []reflect.Value) (reflect.Value, error) {
	ret := reflect.ValueOf(p.fn).Call(args)
	instance, err := ret[0], toError(ret[1])

	return instance, err
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

	// Wait blocks until all current events are processed. Useful for graceful shutdown. It is up to
	// the programmer to ensure that no new events/commands are published. Otherwise, it may run forever.
	Wait()
}

type busImpl struct {
	providers map[reflect.Type]*providerOpts
	listeners map[reflect.Type][]HandlerFunc
	handlers  map[reflect.Type]HandlerFunc
	runnig    sync.WaitGroup
}

func New() Van {
	b := &busImpl{}
	b.providers = make(map[reflect.Type]*providerOpts)
	b.listeners = make(map[reflect.Type][]HandlerFunc)
	b.handlers = make(map[reflect.Type]HandlerFunc)
	b.runnig = sync.WaitGroup{}

	return b
}

func (b *busImpl) Wait() {
	b.runnig.Wait()
}

func (b *busImpl) Provide(provider ProviderFunc) {
	if err := b.registerProvider(provider, false); err != nil {
		panic(err)
	}
}

func (b *busImpl) ProvideSingleton(provider ProviderFunc) {
	if err := b.registerProvider(provider, true); err != nil {
		panic(err)
	}
}

func (b *busImpl) registerProvider(provider ProviderFunc, signleton bool) error {
	providerType := reflect.TypeOf(provider)
	if err := validateProviderSignature(providerType); err != nil {
		return err
	}

	retType := providerType.Out(0)

	for i := 0; i < providerType.NumIn(); i++ {
		inType := providerType.In(i)

		if inType == retType {
			return errInvalidDependency.new("provider function has a dependency of the same type")
		}

		if signleton && inType == typeContext {
			return errInvalidDependency.new("singleton providers cannot use Context as a dependency")
		}

		if err := b.isValidDependency(inType); err != nil {
			return err
		}
	}

	b.providers[retType] = &providerOpts{
		singleton: signleton,
		fn:        provider,
	}

	return nil
}

func (b *busImpl) Handle(cmd interface{}, handler HandlerFunc) {
	if err := b.registerHandler(cmd, handler); err != nil {
		panic(err)
	}
}

func (b *busImpl) registerHandler(cmd interface{}, handler HandlerFunc) error {
	cmdType := reflect.TypeOf(cmd)
	if cmdType.Kind() != reflect.Struct {
		return errInvalidType.fmt("cmd must be a struct, got %s", cmdType.Name())
	}

	handlerType := reflect.TypeOf(handler)
	if err := validateHandlerSignature(handlerType); err != nil {
		return err
	}

	if cmdType != handlerType.In(1).Elem() {
		return errInvalidDependency.new("command type mismatch")
	}

	// start from the third argument as the first two are always `ctx` and `cmd`
	for i := 2; i < handlerType.NumIn(); i++ {
		if err := b.isValidDependency(handlerType.In(i)); err != nil {
			return err
		}
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
		return errInvalidDependency.fmt("no handlers found for type %s", cmdType.String())
	}

	var args []reflect.Value

	handlerType := reflect.TypeOf(handler)
	numIn := handlerType.NumIn()

	if numIn <= maxArgsOnStack {
		var arr [maxArgsOnStack]reflect.Value
		args = arr[:numIn]
	} else {
		args = make([]reflect.Value, numIn)
	}

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
	if err := validateListenerSignature(listenerType); err != nil {
		return err
	}

	if eventType != listenerType.In(1) {
		return errInvalidType.new("event type mismatch")
	}

	// start from the third argument as the first two are always `ctx` and `event`
	for i := 2; i < listenerType.NumIn(); i++ {
		if err := b.isValidDependency(listenerType.In(i)); err != nil {
			return err
		}
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

	var firstErr error

	firstErrOnce := sync.Once{}
	wg := sync.WaitGroup{}

	b.runnig.Add(len(listeners))
	wg.Add(len(listeners))

	for i := range listeners {
		go func(i int) {
			defer wg.Done()
			defer b.runnig.Done()

			var args []reflect.Value

			listenerType := reflect.TypeOf(listeners[i])
			if numIn := listenerType.NumIn(); numIn > 0 {
				if numIn <= maxArgsOnStack {
					var arr [maxArgsOnStack]reflect.Value
					args = arr[:numIn]
				} else {
					args = make([]reflect.Value, numIn)
				}

				err := b.resolve(ctx, event, listenerType, args)
				if err != nil {
					firstErrOnce.Do(func() { firstErr = err })
					return
				}
			}

			reflect.ValueOf(listeners[i]).Call(args)
		}(i)
	}

	wg.Wait()

	return firstErr
}

func (b *busImpl) Exec(ctx context.Context, fn interface{}) error {
	funcType := reflect.TypeOf(fn)
	if err := validateExecLambdaSignature(funcType); err != nil {
		return err
	}

	for i := 0; i < funcType.NumIn(); i++ {
		if err := b.isValidDependency(funcType.In(i)); err != nil {
			return err
		}
	}

	var args []reflect.Value

	numIn := funcType.NumIn()

	if numIn <= maxArgsOnStack {
		var arr [maxArgsOnStack]reflect.Value
		args = arr[:numIn]
	} else {
		args = make([]reflect.Value, numIn)
	}

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
		case argType.Kind() == reflect.Struct:
			value, err := b.buildStruct(ctx, argType)
			if err != nil {
				return err
			}

			args[i] = value
		}
	}

	return nil
}

func (b *busImpl) buildStruct(ctx context.Context, structType reflect.Type) (reflect.Value, error) {
	fields := reflect.VisibleFields(structType)
	value := reflect.New(structType).Elem()

	for _, field := range fields {
		instance, err := b.new(ctx, field.Type)
		if err != nil {
			return reflect.ValueOf(nil), err
		}

		value.FieldByIndex(field.Index).Set(instance)
	}

	return value, nil
}

func (b *busImpl) new(ctx context.Context, t reflect.Type) (reflect.Value, error) {
	provider := b.providers[t]

	if provider.singleton {
		provider.RLock()

		if provider.instance == nil {
			provider.RUnlock()

			return b.newSingleton(ctx, t)
		}

		provider.RUnlock()

		return reflect.ValueOf(provider.instance), nil
	}

	var args []reflect.Value

	providerType := reflect.TypeOf(provider.fn)

	if numIn := providerType.NumIn(); numIn > 0 {
		if numIn <= maxArgsOnStack {
			var arr [maxArgsOnStack]reflect.Value
			args = arr[:numIn]
		} else {
			args = make([]reflect.Value, numIn)
		}

		err := b.resolve(ctx, nil, providerType, args)
		if err != nil {
			return reflect.ValueOf(nil), err
		}
	}

	inst, err := provider.call(args)
	if err != nil {
		return reflect.ValueOf(nil), fmt.Errorf("failed to resolve dependency %s: %w", t.String(), err)
	}

	return inst, nil
}

func (b *busImpl) newSingleton(ctx context.Context, t reflect.Type) (reflect.Value, error) {
	provider := b.providers[t]

	provider.Lock()
	defer provider.Unlock()

	if provider.instance != nil {
		return reflect.ValueOf(provider.instance), nil
	}

	var args []reflect.Value

	providerType := reflect.TypeOf(provider.fn)

	if numIn := providerType.NumIn(); numIn > 0 {
		if numIn <= maxArgsOnStack {
			var arr [maxArgsOnStack]reflect.Value
			args = arr[:numIn]
		} else {
			args = make([]reflect.Value, numIn)
		}

		err := b.resolve(ctx, nil, providerType, args)
		if err != nil {
			return reflect.ValueOf(nil), err
		}
	}

	inst, err := provider.call(args)
	if err != nil {
		return reflect.ValueOf(nil), fmt.Errorf("failed to resolve dependency %s: %w", t.String(), err)
	}

	provider.instance = inst.Interface()

	return inst, nil
}

func (b *busImpl) isValidDependency(t reflect.Type) error {
	if t.Kind() == reflect.Struct {
		for _, field := range reflect.VisibleFields(t) {
			if err := b.isValidDependency(field.Type); err != nil {
				return err
			}
		}

		return nil
	}

	if _, ok := b.providers[t]; ok || t == typeVan || t == typeContext {
		return nil
	}

	return errInvalidDependency.fmt("no providers registered for type %s", t.String())
}
