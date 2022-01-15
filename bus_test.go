package van

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

type Command struct {
	Result int
}
type Event struct {
	Value int
}

type GetIntService interface {
	Get() int
}
type GetIntServiceImpl struct{}

func (s *GetIntServiceImpl) Get() int {
	return 1
}

type SetIntService interface {
	Set(int)
}
type SetIntSevriceImpl struct{}

func (s *SetIntSevriceImpl) Set(i int) {}

type UnknownService interface{}

func panicToError(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	fn()
	return nil
}

func TestProvide(t *testing.T) {
	bus := New().(*busImpl)

	setIntService := &SetIntSevriceImpl{}
	bus.Provide(func() (SetIntService, error) {
		return setIntService, nil
	})

	bus.Provide(func(b Van, s SetIntService) (GetIntService, error) {
		assert.Equal(t, bus, b)
		assert.Equal(t, setIntService, s)
		return &GetIntServiceImpl{}, nil
	})

	assert.Len(t, bus.providers, 2)
}

func TestProvide_NoDeps(t *testing.T) {
	bus := New().(*busImpl)
	bus.Provide(func() (GetIntService, error) {
		return &GetIntServiceImpl{}, nil
	})
	assert.Len(t, bus.providers, 1)
}

func TestProvide_WithContext(t *testing.T) {
	bus := New().(*busImpl)
	bus.Provide(func(ctx context.Context) (GetIntService, error) {
		return &GetIntServiceImpl{}, nil
	})
	assert.Len(t, bus.providers, 1)
}

func TestProvideSingleton(t *testing.T) {
	bus := New().(*busImpl)
	bus.ProvideSingleton(func() (GetIntService, error) {
		return &GetIntServiceImpl{}, nil
	})
	assert.Len(t, bus.providers, 1)

	var opts providerOpts
	for k := range bus.providers {
		opts = bus.providers[k]
		break
	}

	assert.True(t, opts.singleton)
}

func TestProvideFails(t *testing.T) {
	tests := map[string]struct {
		provider   interface{}
		wantErr    error
		wantErrMsg string
	}{
		"not a func": {
			provider:   1,
			wantErr:    errInvalidType,
			wantErrMsg: "provider must be a function, got int",
		},
		"no return value": {
			provider:   func() {},
			wantErr:    errInvalidType,
			wantErrMsg: "provider must have two return values, got 0",
		},
		"too many return values": {
			provider:   func() (int, int, int) { return 1, 2, 3 },
			wantErr:    errInvalidType,
			wantErrMsg: "provider must have two return values, got 3",
		},
		"first return value not an interface": {
			provider:   func() (int, error) { return 1, nil },
			wantErr:    errInvalidType,
			wantErrMsg: "provider's first return value must be an interface, got int",
		},
		"second return value not an error": {
			provider:   func() (GetIntService, int) { return nil, 1 },
			wantErr:    errInvalidType,
			wantErrMsg: "provider's second return value must be an error, got int",
		},
		"arg is not an interface": {
			provider: func(int) (GetIntService, error) {
				return &GetIntServiceImpl{}, nil
			},
			wantErr:    errInvalidType,
			wantErrMsg: "provider's argument 0 must be an interface, got int",
		},
		"unknown interface": {
			provider: func(s SetIntService) (GetIntService, error) {
				return &GetIntServiceImpl{}, nil
			},
			wantErr:    errProviderNotFound,
			wantErrMsg: "no providers registered for type van.SetIntService",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			bus := New()
			err := panicToError(func() {
				bus.Provide(tt.provider)
			})
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantErrMsg, err.Error())
		})
	}
}

func TestHandle(t *testing.T) {
	bus := New().(*busImpl)
	bus.Handle(Command{}, func(ctx context.Context, cmd *Command, bus Van) error {
		return nil
	})
	assert.Len(t, bus.handlers, 1)
}

func TestHandleFails(t *testing.T) {
	tests := map[string]struct {
		cmd        interface{}
		handler    interface{}
		wantErr    error
		wantErrMsg string
	}{
		"msg not a struct": {
			cmd:        1,
			handler:    func() {},
			wantErr:    errInvalidType,
			wantErrMsg: "cmd must be a struct, got int",
		},
		"handler not a func": {
			cmd:        struct{}{},
			handler:    1,
			wantErr:    errInvalidType,
			wantErrMsg: "handler must be a function, got int",
		},
		"less than two args": {
			cmd:        struct{}{},
			handler:    func() error { return nil },
			wantErr:    errInvalidType,
			wantErrMsg: "handler must have at least 2 arguments, got 0",
		},
		"second arg is not a pointer": {
			cmd:        struct{}{},
			handler:    func(context.Context, int) error { return nil },
			wantErr:    errInvalidType,
			wantErrMsg: "handler's second argument must be a struct pointer, got int",
		},
		"second arg is not a struct pointer": {
			cmd:        struct{}{},
			handler:    func(context.Context, *int) error { return nil },
			wantErr:    errInvalidType,
			wantErrMsg: "handler's second argument must be a struct pointer, got *int",
		},
		"no return values": {
			cmd:        struct{}{},
			handler:    func(ctx context.Context, msg *struct{}) {},
			wantErr:    errInvalidType,
			wantErrMsg: "handler must have one return value, got 0",
		},
		"multiple return values": {
			cmd: struct{}{},
			handler: func(ctx context.Context, msg *struct{}) (error, error) {
				return nil, nil
			},
			wantErr:    errInvalidType,
			wantErrMsg: "handler must have one return value, got 2",
		},
		"return type not an error": {
			cmd: struct{}{},
			handler: func(ctx context.Context, msg *struct{}) int {
				return 0
			},
			wantErr:    errInvalidType,
			wantErrMsg: "handler's return type must be error, got int",
		},
		"unknown interface": {
			cmd: struct{}{},
			handler: func(ctx context.Context, msg *struct{}, s SetIntService) error {
				return nil
			},
			wantErr:    errProviderNotFound,
			wantErrMsg: "no providers registered for type van.SetIntService",
		},
		"command type mismatch": {
			cmd: struct{}{},
			handler: func(ctx context.Context, cmd *Command) error {
				return nil
			},
			wantErr:    errInvalidType,
			wantErrMsg: "command type mismatch",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			bus := New()
			err := panicToError(func() {
				bus.Handle(tt.cmd, tt.handler)
			})
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantErrMsg, err.Error())
		})
	}
}

func TestInvoke(t *testing.T) {
	bus := New()
	var providerExecuted, handlerExecuted int
	bus.Provide(func() (SetIntService, error) {
		providerExecuted++
		return &SetIntSevriceImpl{}, nil
	})

	ctx := context.Background()
	bus.Handle(Command{}, func(c context.Context, cmd *Command, s SetIntService) error {
		handlerExecuted++
		return nil
	})

	for i := 0; i < 5; i++ {
		err := bus.Invoke(ctx, &Command{})
		assert.NoError(t, err)
	}

	assert.Equal(t, 5, providerExecuted)
	assert.Equal(t, 5, handlerExecuted)
}

func TestInvokeFails(t *testing.T) {
	tests := map[string]struct {
		cmd        interface{}
		wantErrMsg string
	}{
		"cmd is not a pointer": {
			cmd:        struct{}{},
			wantErrMsg: "cmd must be a pointer to a struct",
		},
		"cmd is not a pointer to struct": {
			cmd: func() *int {
				v := 1
				return &v
			}(),
			wantErrMsg: "cmd must be a pointer to a struct",
		},
		"unregistered handler": {
			cmd:        &Command{},
			wantErrMsg: "no handlers found for type van.Command",
		},
	}

	bus := New()
	ctx := context.Background()
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := bus.Invoke(ctx, tt.cmd)
			assert.Equal(t, tt.wantErrMsg, err.Error())
		})
	}
}

func TestInvokeFails_ProviderError(t *testing.T) {
	bus := New()
	var providerExecuted, handlerExecuted int
	bus.Provide(func() (GetIntService, error) {
		providerExecuted++
		return nil, assert.AnError
	})
	bus.Handle(Command{}, func(ctx context.Context, cmd *Command, s GetIntService) error {
		handlerExecuted++
		return nil
	})

	err := bus.Invoke(context.Background(), &Command{})
	assert.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, 1, providerExecuted)
	assert.Equal(t, 0, handlerExecuted)
}

func TestInvokeFails_HandlerError(t *testing.T) {
	bus := New()
	bus.Handle(Command{}, func(ctx context.Context, cmd *Command) error {
		return assert.AnError
	})

	err := bus.Invoke(context.Background(), &Command{})
	assert.Error(t, assert.AnError, err)
}

func TestHandleEvent(t *testing.T) {
	bus := New().(*busImpl)
	handler := func(ctx context.Context, event Event, b Van) error {
		assert.NotNil(t, b)
		return nil
	}
	bus.Subscribe(Event{}, handler)
	assert.Len(t, bus.listeners, 1)
}

func TestSubscribeFails(t *testing.T) {
	tests := map[string]struct {
		handler    interface{}
		wantErrMsg string
	}{
		"not a function": {
			handler:    struct{}{},
			wantErrMsg: "handler must be a function, got struct {}",
		},
		"not enough arguments": {
			handler:    func() error { return nil },
			wantErrMsg: "handler must have at least 2 arguments, got 0",
		},
		"first argument not a context": {
			handler:    func(ctx struct{}, event Event) error { return nil },
			wantErrMsg: "handler's first argument must be context.Context, got struct {}",
		},
		"second argument not a struct": {
			handler:    func(ctx context.Context, event int) error { return nil },
			wantErrMsg: "handler's second argument must be a struct, got int",
		},
		"dependency is not an interface": {
			handler:    func(ctx context.Context, event Event, dep int) error { return nil },
			wantErrMsg: "handler's argument 2 must be an interface, got int",
		},
		"unknown provider": {
			handler:    func(ctx context.Context, event Event, dep UnknownService) error { return nil },
			wantErrMsg: "no providers registered for type van.UnknownService",
		},
	}
	bus := New()
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := panicToError(func() {
				bus.Subscribe(Event{}, tt.handler)
			})
			assert.Error(t, err)
			assert.Equal(t, tt.wantErrMsg, err.Error())
		})
	}
}

func TestPublish_SingleListener(t *testing.T) {
	var eventTriggered int
	listener := func(ctx context.Context, event Event) error {
		eventTriggered++
		return nil
	}

	bus := New()
	bus.Subscribe(Event{}, listener)
	done, errchan := bus.Publish(context.Background(), Event{})
	<-done

	assert.Len(t, errchan, 0)
	assert.Equal(t, eventTriggered, 1)
}

func TestPublish_MultipleListeners(t *testing.T) {
	var listenerACalled, listenerBCalled int
	listenerA := func(ctx context.Context, event Event) error {
		listenerACalled++
		return nil
	}
	listenerB := func(ctx context.Context, event Event) error {
		listenerBCalled++
		return nil
	}

	bus := New()
	bus.Subscribe(Event{}, listenerA, listenerB)
	done, errchan := bus.Publish(context.Background(), Event{})
	<-done
	assert.Len(t, errchan, 0)
	assert.Equal(t, listenerACalled, 1)
	assert.Equal(t, listenerBCalled, 1)
}

func TestPublish_ListenerFails(t *testing.T) {
	var eventTriggered int
	listener := func(ctx context.Context, event Event) error {
		eventTriggered++
		return nil
	}

	listenerErr := errors.New("arbitrary error")
	badListener := func(ctx context.Context, event Event) error {
		return listenerErr
	}

	bus := New()
	bus.Subscribe(Event{}, badListener, listener)
	done, errchan := bus.Publish(context.Background(), Event{})
	<-done

	assert.Equal(t, eventTriggered, 1)
	assert.Len(t, errchan, 1)
	assert.Equal(t, listenerErr, <-errchan)
}

func TestPublish_ProviderFails(t *testing.T) {
	bus := New()
	var providerExecuted, handlerExecuted int
	bus.Provide(func() (GetIntService, error) {
		providerExecuted++
		return nil, assert.AnError
	})
	bus.Subscribe(Event{}, func(ctx context.Context, event Event, s GetIntService) error {
		handlerExecuted++
		return nil
	})
	done, errchan := bus.Publish(context.Background(), Event{})
	<-done

	assert.Len(t, errchan, 1)
	assert.ErrorIs(t, <-errchan, assert.AnError)
	assert.Equal(t, 1, providerExecuted)
	assert.Equal(t, 0, handlerExecuted)
}

func TestExec_Bus(t *testing.T) {
	bus := New()
	err := bus.Exec(context.Background(), func(b Van) error {
		assert.NotNil(t, b)
		assert.Equal(t, bus, b)
		return nil
	})
	assert.NoError(t, err)
}

func TestExec_ProviderContext(t *testing.T) {
	bus := New()

	ctx := context.Background()
	var providerCalled int
	bus.Provide(func(c context.Context) (GetIntService, error) {
		providerCalled++
		assert.Equal(t, ctx, c)
		return &GetIntServiceImpl{}, nil
	})

	bus.Exec(ctx, func(s GetIntService) error {
		s.Get()
		return nil
	})

	assert.Equal(t, 1, providerCalled)
}

func TestExec_Transitive(t *testing.T) {
	bus := New()

	var providerExecuted, handlerExecuted int
	bus.Provide(func() (GetIntService, error) {
		providerExecuted++
		return &GetIntServiceImpl{}, nil
	})

	for i := 0; i < 5; i++ {
		err := bus.Exec(context.Background(), func(s GetIntService) error {
			assert.NotNil(t, s)
			handlerExecuted++
			return nil
		})
		assert.NoError(t, err)
	}

	assert.Equal(t, 5, providerExecuted)
	assert.Equal(t, 5, handlerExecuted)
}

func TestExec_Singleton(t *testing.T) {
	bus := New()

	var providerExecuted, handlerExecuted int
	bus.ProvideSingleton(func() (GetIntService, error) {
		providerExecuted++
		return &GetIntServiceImpl{}, nil
	})

	for i := 0; i < 5; i++ {
		err := bus.Exec(context.Background(), func(s GetIntService) error {
			assert.NotNil(t, s)
			handlerExecuted++
			return nil
		})
		assert.NoError(t, err)
	}

	assert.Equal(t, 1, providerExecuted)
	assert.Equal(t, 5, handlerExecuted)
}

func TestExec_Concurrent(t *testing.T) {
	bus := New()

	var providerExecuted int
	bus.ProvideSingleton(func() (GetIntService, error) {
		providerExecuted++
		return &GetIntServiceImpl{}, nil
	})

	errchan := make(chan error)
	wg := sync.WaitGroup{}
	wg.Add(5)
	start := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func() {
			<-start
			defer wg.Done()
			err := bus.Exec(context.Background(), func(s GetIntService) error {
				assert.NotNil(t, s)
				return nil
			})
			if err != nil {
				errchan <- err
			}
		}()
	}

	close(start)
	wg.Wait()
	assert.Len(t, errchan, 0)
	assert.Equal(t, 1, providerExecuted)
}
