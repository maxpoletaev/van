package van

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestProvideFails(t *testing.T) {
	tests := map[string]struct {
		provider interface{}
		wantErr  string
	}{
		"not a func": {
			provider: 1,
			wantErr:  "provider must be a function, got int",
		},
		"no return value": {
			provider: func() {},
			wantErr:  "provider must have two return values, got 0",
		},
		"too many return values": {
			provider: func() (int, int, int) { return 1, 2, 3 },
			wantErr:  "provider must have two return values, got 3",
		},
		"first return value not an interface": {
			provider: func() (int, error) { return 1, nil },
			wantErr:  "provider's first return value must be an interface, got int",
		},
		"second return value not an error": {
			provider: func() (GetIntService, int) { return nil, 1 },
			wantErr:  "provider's second return value must be an error, got int",
		},
		"arg is not an interface": {
			provider: func(int) (GetIntService, error) {
				return &GetIntServiceImpl{}, nil
			},
			wantErr: "argument 0 must be an interface or a struct, got int",
		},
		"unknown interface": {
			provider: func(s SetIntService) (GetIntService, error) {
				return &GetIntServiceImpl{}, nil
			},
			wantErr: "no providers registered for type van.SetIntService",
		},
		"dependency of the same type": {
			provider: func(s SetIntService) (SetIntService, error) {
				return s, nil
			},
			wantErr: "provider function has a dependency of the same type",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			bus := New()
			assert.PanicsWithError(t, tt.wantErr, func() {
				bus.Provide(tt.provider)
			})
		})
	}
}

func TestProvideSingleton(t *testing.T) {
	bus := New().(*busImpl)
	bus.ProvideSingleton(func() (GetIntService, error) {
		return &GetIntServiceImpl{}, nil
	})
	assert.Len(t, bus.providers, 1)

	var opts *providerOpts
	for k := range bus.providers {
		opts = bus.providers[k]
		break
	}

	assert.True(t, opts.singleton)
}

func TestProvideSingletonFails(t *testing.T) {
	tests := map[string]struct {
		provider interface{}
		wantErr  string
	}{
		"not a func": {
			provider: 1,
			wantErr:  "provider must be a function, got int",
		},
		"no return value": {
			provider: func() {},
			wantErr:  "provider must have two return values, got 0",
		},
		"too many return values": {
			provider: func() (int, int, int) { return 1, 2, 3 },
			wantErr:  "provider must have two return values, got 3",
		},
		"first return value not an interface": {
			provider: func() (int, error) { return 1, nil },
			wantErr:  "provider's first return value must be an interface, got int",
		},
		"second return value not an error": {
			provider: func() (GetIntService, int) { return nil, 1 },
			wantErr:  "provider's second return value must be an error, got int",
		},
		"arg is not an interface": {
			provider: func(int) (GetIntService, error) {
				return &GetIntServiceImpl{}, nil
			},
			wantErr: "argument 0 must be an interface or a struct, got int",
		},
		"unknown interface": {
			provider: func(s SetIntService) (GetIntService, error) {
				return &GetIntServiceImpl{}, nil
			},
			wantErr: "no providers registered for type van.SetIntService",
		},
		"dependency of the same type": {
			provider: func(s SetIntService) (SetIntService, error) {
				return s, nil
			},
			wantErr: "provider function has a dependency of the same type",
		},
		"context as a dependency": {
			provider: func(ctx context.Context) (SetIntService, error) {
				return &SetIntSevriceImpl{}, nil
			},
			wantErr: "singleton providers cannot use Context as a dependency",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			bus := New()
			assert.PanicsWithError(t, tt.wantErr, func() {
				bus.ProvideSingleton(tt.provider)
			})
		})
	}
}

func TestProvideSingletonFails_ParentProviderTakesContext(t *testing.T) {
	bus := New()
	bus.Provide(func(ctx context.Context) (serviceA, error) { return &serviceImpl{}, nil })
	bus.Provide(func(a serviceA) (serviceB, error) { return &serviceImpl{}, nil })

	assert.PanicsWithError(t, "singleton providers cannot depend on providers that take Context", func() {
		bus.ProvideSingleton(func(b serviceB) (serviceC, error) {
			return &serviceImpl{}, nil
		})
	})
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
		cmd     interface{}
		handler interface{}
		wantErr string
	}{
		"msg not a struct": {
			cmd:     1,
			handler: func() {},
			wantErr: "cmd must be a struct, got int",
		},
		"handler not a func": {
			cmd:     struct{}{},
			handler: 1,
			wantErr: "handler must be a function, got int",
		},
		"less than two args": {
			cmd:     struct{}{},
			handler: func() error { return nil },
			wantErr: "handler must have at least 2 arguments, got 0",
		},
		"second arg is not a pointer": {
			cmd:     struct{}{},
			handler: func(context.Context, int) error { return nil },
			wantErr: "handler's second argument must be a struct pointer, got int",
		},
		"second arg is not a struct pointer": {
			cmd:     struct{}{},
			handler: func(context.Context, *int) error { return nil },
			wantErr: "handler's second argument must be a struct pointer, got *int",
		},
		"no return values": {
			cmd:     struct{}{},
			handler: func(ctx context.Context, msg *struct{}) {},
			wantErr: "handler must have one return value, got 0",
		},
		"multiple return values": {
			cmd: struct{}{},
			handler: func(ctx context.Context, msg *struct{}) (error, error) {
				return nil, nil
			},
			wantErr: "handler must have one return value, got 2",
		},
		"return type not an error": {
			cmd: struct{}{},
			handler: func(ctx context.Context, msg *struct{}) int {
				return 0
			},
			wantErr: "handler's return type must be error, got int",
		},
		"unknown interface": {
			cmd: struct{}{},
			handler: func(ctx context.Context, msg *struct{}, s SetIntService) error {
				return nil
			},
			wantErr: "no providers registered for type van.SetIntService",
		},
		"command type mismatch": {
			cmd: struct{}{},
			handler: func(ctx context.Context, cmd *Command) error {
				return nil
			},
			wantErr: "command type mismatch",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			bus := New()
			assert.PanicsWithError(t, tt.wantErr, func() {
				bus.Handle(tt.cmd, tt.handler)
			})
		})
	}
}

func TestInvoke(t *testing.T) {
	var providerExecuted, handlerExecuted int

	ctx := context.Background()
	bus := New()

	bus.Provide(func() (SetIntService, error) {
		providerExecuted++
		return &SetIntSevriceImpl{}, nil
	})

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

func TestInvoke_StructDeps(t *testing.T) {
	var providerExecuted, handlerExecuted int

	type dependencySet struct {
		S SetIntService
	}

	ctx := context.Background()
	bus := New()

	bus.Provide(func() (SetIntService, error) {
		providerExecuted++
		return &SetIntSevriceImpl{}, nil
	})

	bus.Handle(Command{}, func(c context.Context, cmd *Command, deps dependencySet) error {
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

func TestInvoke_Concurrent(t *testing.T) {
	providerExecuted := make(chan bool, 5)
	handlerExecuted := make(chan bool, 5)

	bus := New()
	bus.Provide(func() (serviceA, error) {
		providerExecuted <- true
		return &serviceImpl{}, nil
	})
	bus.Handle(Command{}, func(c context.Context, cmd *Command, a serviceA) error {
		handlerExecuted <- true
		return nil
	})

	start := make(chan struct{})
	errchan := make(chan error)

	wg := sync.WaitGroup{}
	wg.Add(5)

	for i := 0; i < 5; i++ {
		go func() {
			<-start

			defer wg.Done()

			err := bus.Invoke(context.Background(), &Command{})
			if err != nil {
				errchan <- err
			}
		}()
	}

	close(start)
	wg.Wait()

	assert.Len(t, errchan, 0)
	assert.Len(t, providerExecuted, 5)
	assert.Len(t, handlerExecuted, 5)
}

func TestInvoke_SingletonConcurrent(t *testing.T) {
	providerExecuted := make(chan bool, 5)
	handlerExecuted := make(chan bool, 5)

	bus := New()
	bus.ProvideSingleton(func() (serviceA, error) {
		providerExecuted <- true
		return &serviceImpl{}, nil
	})
	bus.Handle(Command{}, func(c context.Context, cmd *Command, a serviceA) error {
		handlerExecuted <- true
		return nil
	})

	start := make(chan struct{})
	errchan := make(chan error)

	wg := sync.WaitGroup{}
	wg.Add(5)

	for i := 0; i < 5; i++ {
		go func() {
			<-start

			defer wg.Done()

			err := bus.Invoke(context.Background(), &Command{})
			if err != nil {
				errchan <- err
			}
		}()
	}

	close(start)
	wg.Wait()

	assert.Len(t, errchan, 0)
	assert.Len(t, providerExecuted, 1)
	assert.Len(t, handlerExecuted, 5)
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
	var providerExecuted, handlerExecuted int

	bus := New()
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
	handler := func(ctx context.Context, event Event, b Van) {
		assert.NotNil(t, b)
	}
	bus.Subscribe(Event{}, handler)
	assert.Len(t, bus.listeners, 1)
}

func TestSubscribeFails(t *testing.T) {
	tests := map[string]struct {
		handler interface{}
		wantErr string
	}{
		"not a function": {
			handler: struct{}{},
			wantErr: "handler must be a function, got struct {}",
		},
		"not enough arguments": {
			handler: func() {},
			wantErr: "handler must have at least 2 arguments, got 0",
		},
		"first argument not a context": {
			handler: func(ctx struct{}, event Event) {},
			wantErr: "handler's first argument must be context.Context, got struct {}",
		},
		"second argument not a struct": {
			handler: func(ctx context.Context, event int) {},
			wantErr: "handler's second argument must be a struct, got int",
		},
		"dependency is not an interface": {
			handler: func(ctx context.Context, event Event, dep int) {},
			wantErr: "argument 2 must be an interface or a struct, got int",
		},
		"unknown provider": {
			handler: func(ctx context.Context, event Event, dep UnknownService) {},
			wantErr: "no providers registered for type van.UnknownService",
		},
		"has return values": {
			handler: func(ctx context.Context, event Event) error { return nil },
			wantErr: "event handler should not have any return values",
		},
	}

	bus := New()

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.PanicsWithError(t, tt.wantErr, func() {
				bus.Subscribe(Event{}, tt.handler)
			})
		})
	}
}

func TestPublish_SingleListener(t *testing.T) {
	var eventTriggered int

	listener := func(ctx context.Context, event Event) {
		eventTriggered++
	}

	bus := New()
	bus.Subscribe(Event{}, listener)
	err := bus.Publish(context.Background(), Event{})

	require.NoError(t, err)
	assert.Equal(t, 1, eventTriggered)
}

func TestPublish_MultipleListeners(t *testing.T) {
	var listenerACalled, listenerBCalled int

	listenerA := func(ctx context.Context, event Event) {
		listenerACalled++
	}
	listenerB := func(ctx context.Context, event Event) {
		listenerBCalled++
	}

	bus := New()
	bus.Subscribe(Event{}, listenerA, listenerB)
	err := bus.Publish(context.Background(), Event{})

	require.NoError(t, err)
	assert.Equal(t, 1, listenerACalled)
	assert.Equal(t, 1, listenerBCalled)
}

func TestPublish_ProviderFails(t *testing.T) {
	var providerExecuted, handlerExecuted int

	bus := New()
	bus.Provide(func() (GetIntService, error) {
		providerExecuted++
		return nil, assert.AnError
	})
	bus.Subscribe(Event{}, func(ctx context.Context, event Event, s GetIntService) {
		handlerExecuted++
	})

	err := bus.Publish(context.Background(), Event{})

	assert.ErrorIs(t, err, assert.AnError)
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
	var providerCalled int

	ctx := context.Background()
	bus := New()

	bus.Provide(func(c context.Context) (GetIntService, error) {
		providerCalled++
		assert.Equal(t, ctx, c)
		return &GetIntServiceImpl{}, nil
	})

	err := bus.Exec(ctx, func(s GetIntService) error {
		s.Get()
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, providerCalled)
}

func TestExec_Transitive(t *testing.T) {
	var providerExecuted, handlerExecuted int

	bus := New()
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
	var providerExecuted, handlerExecuted int

	bus := New()
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
	var providerExecuted int

	bus := New()
	bus.ProvideSingleton(func() (GetIntService, error) {
		providerExecuted++
		return &GetIntServiceImpl{}, nil
	})

	start := make(chan struct{})
	errchan := make(chan error)

	wg := sync.WaitGroup{}
	wg.Add(5)

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

func TestExec_Fails(t *testing.T) {
	tests := map[string]struct {
		fn      interface{}
		wantErr string
	}{
		"unknown provider": {
			fn:      func(dep UnknownService) error { return nil },
			wantErr: "no providers registered for type van.UnknownService",
		},
		"invalid signature": {
			fn:      func() {},
			wantErr: "fn must have one return value, got 0",
		},
	}

	ctx := context.Background()
	bus := New()

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := bus.Exec(ctx, tt.fn)
			assert.Error(t, err)
			assert.EqualError(t, err, tt.wantErr)
		})
	}
}
