package van

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

type providerFunc interface{} // func(deps ...interface{}) interface{}
type handlerFunc interface{}  // func(ctx context.Context, cmd interface{}, deps ...interface{}) error

type providerOpts struct {
	fn        providerFunc
	singleton bool
}

type Van interface {
	Provide(provider providerFunc)
	ProvideSingleton(provider providerFunc)
	HandleCommand(cmd interface{}, handler handlerFunc)
	InvokeCommand(ctx context.Context, cmd interface{}) error
	ListenEvent(event interface{}, listeners ...handlerFunc)
	EmitEvent(ctx context.Context, event interface{}) (chan struct{}, chan error)
	Resolve(fn interface{}) error
}

type busImpl struct {
	providers   map[reflect.Type]providerOpts
	handlers    map[reflect.Type]handlerFunc
	listeners   map[reflect.Type][]handlerFunc
	instances   map[reflect.Type]interface{}
	instancesMu sync.RWMutex
	selfType    reflect.Type
}

func New() Van {
	b := &busImpl{}
	b.providers = make(map[reflect.Type]providerOpts)
	b.handlers = make(map[reflect.Type]handlerFunc)
	b.listeners = make(map[reflect.Type][]handlerFunc)
	b.instances = make(map[reflect.Type]interface{})
	b.selfType = reflect.TypeOf((*Van)(nil)).Elem()
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

func (b *busImpl) HandleCommand(cmd interface{}, handler handlerFunc) {
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

func (b *busImpl) InvokeCommand(ctx context.Context, cmd interface{}) error {
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
	for i := range listeners {
		err := b.registerListener(event, listeners[i])
		if err != nil {
			panic(err)
		}
	}
}

func (b *busImpl) registerListener(event interface{}, listener handlerFunc) error {
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

func (b *busImpl) EmitEvent(ctx context.Context, event interface{}) (done chan struct{}, errchan chan error) {
	done = make(chan struct{})
	errchan = make(chan error, 1)

	eventType := reflect.TypeOf(event)
	if eventType.Kind() != reflect.Struct {
		errchan <- ErrInvalidType.new("event must be a a struct")
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

func (b *busImpl) Resolve(fn interface{}) error {
	funcType := reflect.TypeOf(fn)
	switch {
	case funcType.Kind() != reflect.Func:
		return ErrInvalidType.new("fn should be a function, got " + funcType.String())
	case funcType.NumOut() != 1:
		return ErrInvalidType.new("fn must have one return value, got " + fmt.Sprint(funcType.NumOut()))
	case !isTypeError(funcType.Out(0)):
		return ErrInvalidType.new("return value must be an error, got" + funcType.Out(0).String())
	}

	for i := 0; i < funcType.NumIn(); i++ {
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

		// dependency is the bus itself
		if argType == b.selfType {
			args[i] = reflect.ValueOf(b)
			continue
		}

		// dependency is a singleton
		if _, ok := b.instances[argType]; ok {
			b.instancesMu.RLock()
			args[i] = reflect.ValueOf(b.instances[argType])
			b.instancesMu.RUnlock()
			continue
		}

		provider := b.providers[argType]
		providerType := reflect.TypeOf(provider.fn)
		providerValue := reflect.ValueOf(provider.fn)

		nextArgs := make([]reflect.Value, providerType.NumIn())
		err := b.resolve(providerValue, nextArgs)
		if err != nil {
			return err
		}

		instance := providerValue.Call(nextArgs)[0]
		if provider.singleton {
			b.instancesMu.Lock()
			b.instances[argType] = instance.Interface()
			b.instancesMu.Unlock()
		}

		args[i] = instance
	}

	return nil
}

func (b *busImpl) validateProviderType(t reflect.Type) error {
	switch {
	case t.Kind() != reflect.Func:
		return ErrInvalidType.new("provider must be a function, got " + t.String())
	case t.NumOut() != 1:
		return ErrInvalidType.new("provider must have one return value, got " + fmt.Sprint(t.NumOut()))
	}

	retType := t.Out(0)
	switch {
	case retType.Kind() != reflect.Interface:
		return ErrInvalidType.new("provider's return value must be an interface, got " + retType.String())
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
	case !isTypeContext(t.In(0)):
		return ErrInvalidType.new("handler's first argument must be context.Context, got " + t.In(0).String())
	case !isTypeStructPointer(t.In(1)):
		return ErrInvalidType.new("handler's second argument must be a struct pointer, got " + t.In(1).String())
	case t.NumOut() != 1:
		return ErrInvalidType.new("handler must have one return value, got " + fmt.Sprint(t.NumOut()))
	}

	retType := t.Out(0)
	switch {
	case !isTypeError(retType):
		return ErrInvalidType.new("handler's return type must be error, got " + retType.String())
	}

	// make sure all the dependencies are interfaces and have registered providers
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
	case !isTypeContext(t.In(0)):
		return ErrInvalidType.new("handler's first argument must be context.Context, got " + t.In(0).String())
	case t.In(1).Kind() != reflect.Struct:
		return ErrInvalidType.new("handler's second argument must be a struct, got " + t.In(1).String())
	}

	// make sure all the dependencies are interfaces and have registered providers
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
	if _, ok := b.providers[t]; ok || t == b.selfType {
		return true
	}
	return false
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
