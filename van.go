package van

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"sync"
)

// MaxArgs is the maximum number of arguments (dependencies) a function can have.
// Since we don't want to allocate a dynamic slice for every function call, we use
// a fixed size array. One can always bypass this limitation by using a dependency struct.
const MaxArgs = 16

type ProviderFunc interface{} // func(ctx context.Context, deps ...interface{}) (interface{}, error)
type HandlerFunc interface{}  // func(ctx context.Context, cmd interface{}, deps ...interface{}) error
type ListenerFunc interface{} // func(ctx context.Context, event interface{}, deps ...interface)

type providerOpts struct {
	sync.RWMutex

	fn           ProviderFunc
	instance     interface{}
	singleton    bool
	takesContext bool
}

func (p *providerOpts) call(args []reflect.Value) (reflect.Value, error) {
	ret := reflect.ValueOf(p.fn).Call(args)
	instance, err := ret[0], toError(ret[1])

	return instance, err
}

type Van struct {
	providers map[reflect.Type]*providerOpts
	listeners map[reflect.Type][]HandlerFunc
	handlers  map[reflect.Type]HandlerFunc
	wg        sync.WaitGroup
}

func New() *Van {
	return &Van{
		providers: make(map[reflect.Type]*providerOpts),
		listeners: make(map[reflect.Type][]HandlerFunc),
		handlers:  make(map[reflect.Type]HandlerFunc),
	}
}

// Wait blocks until all current events are processed, which may be used for implementing graceful shutdown.
// It is up to the programmer to ensure that no new events/commands are published, otherwise it may run forever.
func (b *Van) Wait() {
	b.wg.Wait()
}

// Provide registers new type constructor that will be called every time a handler requests the dependency.
// There's no such thing as "optional" dependency. Therefore, the provider should either return a valid non-nil
// dependency or an error.
// It is expected to be called during the app startup phase as it performs the run time type checking and
// panics if an incorrect function type is provided.
func (b *Van) Provide(provider ProviderFunc) {
	if err := b.registerProvider(provider, false); err != nil {
		panic(err)
	}
}

// ProvideSingleton registers a new type constructor that is guaranteed to be called not more than once in
// application's lifetime.
// It is expected to be called during the app startup phase as it performs the run time type checking and
// panics if an incorrect function type is provided.
func (b *Van) ProvideSingleton(provider ProviderFunc) {
	if err := b.registerProvider(provider, true); err != nil {
		panic(err)
	}
}

func (b *Van) registerProvider(provider ProviderFunc, signleton bool) error {
	providerType := reflect.TypeOf(provider)
	if err := validateProviderSignature(providerType); err != nil {
		return err
	}

	retType := providerType.Out(0)
	takesContext := false

	for i := 0; i < providerType.NumIn(); i++ {
		inType := providerType.In(i)

		if inType == retType {
			return fmt.Errorf("provider function has a dependency of the same type")
		}

		if err := b.validateDependency(inType); err != nil {
			return err
		}

		if inType == typeContext {
			if signleton {
				return fmt.Errorf("singleton providers cannot use Context as a dependency")
			}

			takesContext = true
		}

		if pp, ok := b.providers[inType]; ok && pp.takesContext {
			if signleton {
				return fmt.Errorf("singleton providers cannot depend on providers that take Context")
			}

			takesContext = true
		}
	}

	b.providers[retType] = &providerOpts{
		fn:           provider,
		singleton:    signleton,
		takesContext: takesContext,
	}

	return nil
}

// Handle registers a handler for the given command type. There can be only one handler per command.
// It is expected to be called during the app startup phase as it performs the run time type checking and
// panics if an incorrect function type is provided.
func (b *Van) Handle(cmd interface{}, handler HandlerFunc) {
	if err := b.registerHandler(cmd, handler); err != nil {
		panic(err)
	}
}

func (b *Van) registerHandler(cmd interface{}, handler HandlerFunc) error {
	cmdType := reflect.TypeOf(cmd)
	if cmdType.Kind() != reflect.Struct {
		return fmt.Errorf("cmd must be a struct, got %s", cmdType.Name())
	}

	handlerType := reflect.TypeOf(handler)
	if err := validateHandlerSignature(handlerType); err != nil {
		return err
	}

	if cmdType != handlerType.In(1).Elem() {
		return fmt.Errorf("command type mismatch")
	}

	// start from the third argument as the first two are always `ctx` and `cmd`
	for i := 2; i < handlerType.NumIn(); i++ {
		if err := b.validateDependency(handlerType.In(i)); err != nil {
			return err
		}
	}

	b.handlers[cmdType] = handler

	return nil
}

// Invoke runs an associated command handler.
func (b *Van) Invoke(ctx context.Context, cmd interface{}) error {
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

	var args [MaxArgs]reflect.Value

	handlerType := reflect.TypeOf(handler)

	numIn := handlerType.NumIn()

	if numIn > len(args) {
		return fmt.Errorf("too many dependencies for handler %s", handlerType.String())
	}

	err := b.resolve(ctx, cmd, handlerType, args[:numIn])
	if err != nil {
		return err
	}

	ret := reflect.ValueOf(handler).Call(args[:numIn])

	return toError(ret[0])
}

// Subscribe registers a new handler for the given command type. There can be any number of handlers per event.
// It is expected to be called during the app startup phase as it performs the run time type checking and
// panics if an incorrect function type is provided.
func (b *Van) Subscribe(event interface{}, listeners ...ListenerFunc) {
	for i := range listeners {
		err := b.registerListener(event, listeners[i])
		if err != nil {
			panic(err)
		}
	}
}

func (b *Van) registerListener(event interface{}, listener ListenerFunc) error {
	eventType := reflect.TypeOf(event)
	if eventType.Kind() != reflect.Struct {
		return fmt.Errorf("event must be a struct, got %s", eventType.String())
	}

	listenerType := reflect.TypeOf(listener)
	if err := validateListenerSignature(listenerType); err != nil {
		return err
	}

	if eventType != listenerType.In(1) {
		return fmt.Errorf("event type mismatch")
	}

	// start from the third argument as the first two are always `ctx` and `event`
	for i := 2; i < listenerType.NumIn(); i++ {
		if err := b.validateDependency(listenerType.In(i)); err != nil {
			return err
		}
	}

	if _, ok := b.listeners[eventType]; !ok {
		b.listeners[eventType] = make([]HandlerFunc, 0)
	}

	b.listeners[eventType] = append(b.listeners[eventType], listener)

	return nil
}

// Publish sends an event to the bus. This is a fire-and-forget non-blocking operation.
// Each listener will be called in a separate goroutine, and they can fail independently.
// The error is never propagated back to the publisher, and should be handled by the listener itself.
func (b *Van) Publish(ctx context.Context, event interface{}) error {
	eventType := reflect.TypeOf(event)
	if eventType.Kind() != reflect.Struct {
		return fmt.Errorf("event must be a a struct, got %s", eventType.Name())
	}

	b.wg.Add(1)

	go func() {
		defer b.wg.Done()
		b.processEvent(ctx, event)
	}()

	return nil
}

func (b *Van) processEvent(ctx context.Context, event interface{}) {
	eventType := reflect.TypeOf(event)

	listeners, ok := b.listeners[eventType]
	if !ok || len(listeners) == 0 {
		return
	}

	for i := range listeners {
		typ := reflect.TypeOf(listeners[i])

		var args [MaxArgs]reflect.Value

		numIn := typ.NumIn()

		if numIn > len(args) {
			log.Printf("van: too many dependencies for listener %s", typ.String())
			continue
		}

		if numIn > 0 {
			err := b.resolve(ctx, event, typ, args[:numIn])
			if err != nil {
				log.Printf("van: failed to resolve dependencies for %s: %s", typ.String(), err)
				continue
			}
		}

		reflect.ValueOf(listeners[i]).Call(args[:numIn])
	}
}

// Exec executes the given function inside the dependency injector.
func (b *Van) Exec(ctx context.Context, fn interface{}) error {
	funcType := reflect.TypeOf(fn)
	if err := validateExecLambdaSignature(funcType); err != nil {
		return err
	}

	for i := 0; i < funcType.NumIn(); i++ {
		if err := b.validateDependency(funcType.In(i)); err != nil {
			return err
		}
	}

	var args [MaxArgs]reflect.Value

	numIn := funcType.NumIn()

	if numIn > len(args) {
		return fmt.Errorf("too many dependencies for function %s", funcType.String())
	}

	err := b.resolve(ctx, nil, funcType, args[:numIn])
	if err != nil {
		return err
	}

	ret := reflect.ValueOf(fn).Call(args[:numIn])

	return toError(ret[0])
}

func (b *Van) resolve(ctx context.Context, cmd interface{}, funcType reflect.Type, args []reflect.Value) error {
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
		default:
		}
	}

	return nil
}

func (b *Van) buildStruct(ctx context.Context, structType reflect.Type) (reflect.Value, error) {
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

func (b *Van) new(ctx context.Context, t reflect.Type) (reflect.Value, error) {
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

	providerType := reflect.TypeOf(provider.fn)

	var args [MaxArgs]reflect.Value

	numIn := providerType.NumIn()

	if numIn > len(args) {
		return reflect.ValueOf(nil), fmt.Errorf("too many dependencies for provider %s", providerType.String())
	}

	if numIn > 0 {
		err := b.resolve(ctx, nil, providerType, args[:numIn])
		if err != nil {
			return reflect.ValueOf(nil), err
		}
	}

	inst, err := provider.call(args[:numIn])
	if err != nil {
		return reflect.ValueOf(nil), fmt.Errorf("failed to resolve dependency %s: %w", t.String(), err)
	}

	return inst, nil
}

func (b *Van) newSingleton(ctx context.Context, t reflect.Type) (reflect.Value, error) {
	provider := b.providers[t]

	provider.Lock()
	defer provider.Unlock()

	if provider.instance != nil {
		return reflect.ValueOf(provider.instance), nil
	}

	providerType := reflect.TypeOf(provider.fn)

	var args [MaxArgs]reflect.Value

	numIn := providerType.NumIn()

	if numIn > len(args) {
		return reflect.ValueOf(nil), fmt.Errorf("too many dependencies for provider %s", providerType.String())
	}

	if numIn > 0 {
		err := b.resolve(ctx, nil, providerType, args[:numIn])
		if err != nil {
			return reflect.ValueOf(nil), err
		}
	}

	inst, err := provider.call(args[:numIn])
	if err != nil {
		return reflect.ValueOf(nil), fmt.Errorf("failed to resolve dependency %s: %w", t.String(), err)
	}

	provider.instance = inst.Interface()

	return inst, nil
}

func (b *Van) validateDependency(t reflect.Type) error {
	if t.Kind() == reflect.Struct {
		for _, field := range reflect.VisibleFields(t) {
			if err := b.validateDependency(field.Type); err != nil {
				return err
			}
		}

		return nil
	}

	if _, ok := b.providers[t]; ok || t == typeVan || t == typeContext {
		return nil
	}

	return fmt.Errorf("no providers registered for type %s", t.String())
}
