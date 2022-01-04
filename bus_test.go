package van

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type Command struct {
	Value int
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
	bus.Provide(func() GetIntService {
		return &GetIntServiceImpl{}
	})
	assert.Len(t, bus.providers, 2)
}

func TestProvide_WithDeps(t *testing.T) {
	bus := New().(*busImpl)

	setIntService := &SetIntSevriceImpl{}
	bus.Provide(func() SetIntService {
		return setIntService
	})

	bus.Provide(func(s SetIntService) GetIntService {
		assert.Equal(t, setIntService, s)
		return &GetIntServiceImpl{}
	})

	assert.Len(t, bus.providers, 3)
}

func TestBus_ProvideFails(t *testing.T) {
	tests := map[string]struct {
		provider  interface{}
		wantPanic string
	}{
		"not a func": {
			provider:  1,
			wantPanic: "provider must be a function",
		},
		"no return value": {
			provider:  func() {},
			wantPanic: "provider must have one return value",
		},
		"multiple return values": {
			provider:  func() (int, int) { return 1, 2 },
			wantPanic: "provider must have one return value",
		},
		"return value not an interface": {
			provider:  func() int { return 1 },
			wantPanic: "provider's return value must be an interface",
		},
		"arg is not an interface": {
			provider: func(int) GetIntService {
				return &GetIntServiceImpl{}
			},
			wantPanic: "provider's argument 0 must be an interface",
		},
		"unknown interface": {
			provider: func(s SetIntService) GetIntService {
				return &GetIntServiceImpl{}
			},
			wantPanic: "no providers registered for type van.SetIntService",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			bus := New()
			assert.PanicsWithValue(t, tt.wantPanic, func() {
				bus.Provide(tt.provider)
			})
		})
	}
}

func TesHandleCommand(t *testing.T) {
	bus := New().(*busImpl)
	type cmd struct{}
	bus.HandleCommand(cmd{}, func(ctx context.Context, cmd *cmd) error {
		return nil
	})
	assert.Len(t, bus.handlers, 1)
}

func TesHandleCommandFails(t *testing.T) {
	tests := map[string]struct {
		cmd       interface{}
		handler   interface{}
		wantPanic string
	}{
		"msg not a struct": {
			cmd:       1,
			handler:   func() {},
			wantPanic: "msg must be a struct",
		},
		"handler not a func": {
			cmd:       struct{}{},
			handler:   1,
			wantPanic: "handler must be a function",
		},
		"less than two args": {
			cmd:       struct{}{},
			handler:   func() error { return nil },
			wantPanic: "handler must have at least 2 arguments",
		},
		"second arg is not a pointer": {
			cmd:       struct{}{},
			handler:   func(context.Context, int) error { return nil },
			wantPanic: "handler's second argument must be a struct pointer",
		},
		"second arg is not a struct": {
			cmd:       struct{}{},
			handler:   func(context.Context, *int) error { return nil },
			wantPanic: "handler's second argument must be a struct pointer",
		},
		"no return values": {
			cmd:       struct{}{},
			handler:   func(ctx context.Context, msg *struct{}) {},
			wantPanic: "handler must have one return value",
		},
		"multiple return values": {
			cmd: struct{}{},
			handler: func(ctx context.Context, msg *struct{}) (error, error) {
				return nil, nil
			},
			wantPanic: "handler must have one return value",
		},
		"return value not an error": {
			cmd: struct{}{},
			handler: func(ctx context.Context, msg *struct{}) int {
				return 0
			},
			wantPanic: "handler's return value must be an error",
		},
		"unknown interface": {
			cmd: struct{}{},
			handler: func(ctx context.Context, msg *struct{}, s SetIntService) error {
				return nil
			},
			wantPanic: "no providers registered for type van.SetIntService",
		},
		"command type mismatch": {
			cmd: struct{}{},
			handler: func(ctx context.Context, cmd *Command) error {
				return nil
			},
			wantPanic: "command type mismatch",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			bus := New()
			assert.PanicsWithValue(t, tt.wantPanic, func() {
				bus.HandleCommand(tt.cmd, tt.handler)
			})
		})
	}
}

func TestHandleCommandFails_AlreadyRegistered(t *testing.T) {
	bus := New()
	handler := func(ctx context.Context, cmd *Command) error {
		return nil
	}
	bus.HandleCommand(Command{}, handler)
	assert.PanicsWithValue(t, "handler already registered for type van.Command", func() {
		bus.HandleCommand(Command{}, handler)
	})
}

func TestInvokeCommand(t *testing.T) {
	bus := New()
	var providerExecuted, handlerExecuted int
	bus.Provide(func() SetIntService {
		providerExecuted++
		return &SetIntSevriceImpl{}
	})

	bus.HandleCommand(Command{}, func(ctx context.Context, cmd *Command, s SetIntService) error {
		handlerExecuted++
		return nil
	})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		err := bus.InvokeCommand(ctx, &Command{})
		assert.NoError(t, err)
	}

	assert.Equal(t, providerExecuted, 5)
	assert.Equal(t, handlerExecuted, 5)
}

func TestInvokeCommandFails(t *testing.T) {
	tests := map[string]struct {
		cmd     interface{}
		wantErr string
	}{
		"cmd is not a pointer": {
			cmd:     struct{}{},
			wantErr: "cmd must be a pointer to a struct",
		},
		"cmd is not a pointer to struct": {
			cmd: func() *int {
				v := 1
				return &v
			}(),
			wantErr: "cmd must be a pointer to a struct",
		},
		"unregistered handler": {
			cmd:     &Command{},
			wantErr: "no handlers found for type van.Command",
		},
	}

	bus := New()
	ctx := context.Background()
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := bus.InvokeCommand(ctx, tt.cmd)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}

func TestListenEvent(t *testing.T) {
	bus := New().(*busImpl)
	handler := func(ctx context.Context, event Event) error {
		return nil
	}
	bus.ListenEvent(Event{}, handler)
	assert.Len(t, bus.listeners, 1)
}

func TestListenEvenFails(t *testing.T) {
	tests := map[string]struct {
		handler   interface{}
		wantPanic string
	}{
		"not a function": {
			handler:   struct{}{},
			wantPanic: "listener must be a function",
		},
		"not enough arguments": {
			handler:   func() error { return nil },
			wantPanic: "listener must have at least 2 arguments",
		},
		"first argument not a context": {
			handler:   func(ctx struct{}, event Event) error { return nil },
			wantPanic: "listener's first argument must be the context",
		},
		"second argument not a struct": {
			handler:   func(ctx context.Context, event int) error { return nil },
			wantPanic: "listener's second argument must be a struct",
		},
		"dependency is not an interface": {
			handler:   func(ctx context.Context, event Event, dep int) error { return nil },
			wantPanic: "listener's argument 2 must be an interface",
		},
		"unknown provider": {
			handler:   func(ctx context.Context, event Event, dep UnknownService) error { return nil },
			wantPanic: "no providers registered for type van.UnknownService",
		},
	}
	bus := New()
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.PanicsWithValue(t, tt.wantPanic, func() {
				bus.ListenEvent(Event{}, tt.handler)
			})
		})
	}
}

func TestEmitEvent_SingleListener(t *testing.T) {
	var eventTriggered int
	listener := func(ctx context.Context, event Event) error {
		eventTriggered++
		return nil
	}

	bus := New()
	bus.ListenEvent(Event{}, listener)
	done, errchan := bus.EmitEvent(context.Background(), Event{})
	<-done
	assert.Len(t, errchan, 0)
	assert.Equal(t, eventTriggered, 1)
}

func TestEmitEvent_MultipleListeners(t *testing.T) {
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
	bus.ListenEvent(Event{}, listenerA, listenerB)
	done, errchan := bus.EmitEvent(context.Background(), Event{})
	<-done
	assert.Len(t, errchan, 0)
	assert.Equal(t, listenerACalled, 1)
	assert.Equal(t, listenerBCalled, 1)
}

func TestEmitEvent_OneListenerFails(t *testing.T) {
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
	bus.ListenEvent(Event{}, badListener, listener)
	done, errchan := bus.EmitEvent(context.Background(), Event{Value: 1})
	<-done

	assert.Equal(t, eventTriggered, 1)
	assert.Len(t, errchan, 1)
	assert.Equal(t, listenerErr, <-errchan)
}
