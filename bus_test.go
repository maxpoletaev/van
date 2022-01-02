package gobus

import (
	"context"
	"testing"
)

type service interface {
	Get() int
}

type serviceImpl struct{}

func (s *serviceImpl) Get() int {
	return 1
}

func assertPanic(t *testing.T, p interface{}, message string) {
	v, ok := p.(string)
	if !ok {
		t.Errorf("expected panic with message '%s', got '%v'", message, p)
		return
	}
	if v != message {
		t.Errorf("expected panic with message '%s', got '%v'", message, v)
	}
}

func TestBus_Provide(t *testing.T) {
	bus := New()
	bus.Provide(func() service {
		return &serviceImpl{}
	})
	if len(bus.providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(bus.providers))
	}
}

func TestBus_ProvideFails(t *testing.T) {
	tests := map[string]struct {
		msg           interface{}
		handler       interface{}
		expectedPanic string
	}{
		"msg not a struct": {
			msg:           1,
			expectedPanic: "msg must be a struct",
		},
		"no return value": {
			msg:           struct{}{},
			handler:       func() {},
			expectedPanic: "handler must have one retunrn value",
		},
		"return value not an interface": {
			msg:           struct{}{},
			handler:       func() int { return 1 },
			expectedPanic: "handler's return value must be an interface",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					assertPanic(t, r, tt.expectedPanic)
				}
			}()
			bus := New()
			bus.Handle(tt.msg, tt.handler)
		})
	}
}

func TestBus_Handle(t *testing.T) {
	bus := New()
	type cmd struct{}
	bus.Handle(cmd{}, func(ctx context.Context, cmd *cmd) error {
		return nil
	})
	if len(bus.handlers) != 1 {
		t.Errorf("expected 1 handler, got %d", len(bus.handlers))
	}
}

func TestBus_HandleFails(t *testing.T) {
	type args struct {
		msg     interface{}
		handler interface{}
	}
	tests := map[string]struct {
		args      args
		wantPanic string
	}{
		"msg not a struct": {
			args: args{
				msg:     1,
				handler: func() {},
			},
			wantPanic: "msg must be a struct",
		},
		"handler not a func": {
			args: args{
				msg:     struct{}{},
				handler: 1,
			},
			wantPanic: "handler must be a function",
		},
		"handler must have one return value": {
			args: args{
				msg:     struct{}{},
				handler: func(ctx context.Context, msg struct{}) {},
			},
			wantPanic: "handler must have one return value",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					assertPanic(t, r, tt.wantPanic)
				}
			}()
			bus := New()
			bus.Handle(tt.args.msg, tt.args.handler)
		})
	}
}
