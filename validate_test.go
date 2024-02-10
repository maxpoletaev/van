package van

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsStructPtr(t *testing.T) {
	assert.True(t, isStructPtr(reflect.TypeOf(&struct{}{})))
	assert.False(t, isStructPtr(reflect.TypeOf(struct{}{})))
	assert.False(t, isStructPtr(reflect.TypeOf(1)))
}

func TestValidateProviderSignature(t *testing.T) {
	tests := map[string]struct {
		provider interface{}
		wantErr  string
		wantOk   bool
	}{
		"valid provider": {
			provider: func(context.Context) (interface{}, error) { return nil, nil },
			wantOk:   true,
		},
		"not a function": {
			provider: 0,
			wantErr:  "provider must be a function, got int",
		},
		"no return values": {
			provider: func(context.Context) {},
			wantErr:  "provider must have two return values, got 0",
		},
		"too many return values": {
			provider: func(context.Context) (interface{}, interface{}, error) { return nil, nil, nil },
			wantErr:  "provider must have two return values, got 3",
		},
		"first return value not interface": {
			provider: func(context.Context) (int, error) { return 0, nil },
			wantErr:  "provider's first return value must be an interface, got int",
		},
		"second return value not error": {
			provider: func(context.Context) (interface{}, int) { return nil, 0 },
			wantErr:  "provider's second return value must be an error, got int",
		},
		"argument not interface": {
			provider: func(context.Context, int) (interface{}, error) { return nil, nil },
			wantErr:  "argument 1 must be an interface, struct or *van.Van, got int",
		},
		"dependency struct field is not exported": {
			provider: func(context.Context, struct{ s interface{} }) (interface{}, error) { return nil, nil },
			wantErr:  "error in dependency struct argument 1: field s must be exported",
		},
		"dependency struct field is not an interface": {
			provider: func(context.Context, struct{ S int }) (interface{}, error) { return nil, nil },
			wantErr:  "error in dependency struct argument 1: field S must be an interface, got int",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			providerType := reflect.TypeOf(tt.provider)
			err := validateProviderSignature(providerType)
			if tt.wantOk {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestValidateHandlerSignature(t *testing.T) {
	tests := map[string]struct {
		handler interface{}
		wantErr string
		wantOk  bool
	}{
		"valid handler": {
			handler: func(context.Context, *struct{}, interface{}) error { return nil },
			wantOk:  true,
		},
		"not a function": {
			handler: 0,
			wantErr: "handler must be a function, got int",
		},
		"not enough arguments": {
			handler: func(context.Context) error { return nil },
			wantErr: "handler must have at least 2 arguments, got 1",
		},
		"first argument is not a not context": {
			handler: func(int, *struct{}, interface{}) error { return nil },
			wantErr: "handler's first argument must be context.Context, got int",
		},
		"second argument is not a pointer to struct": {
			handler: func(context.Context, int, interface{}) error { return nil },
			wantErr: "handler's second argument must be a struct pointer, got int",
		},
		"third argument is not an interface": {
			handler: func(context.Context, *struct{}, int) error { return nil },
			wantErr: "argument 2 must be an interface, struct or *van.Van, got int",
		},
		"dependency struct field is not exported": {
			handler: func(context.Context, *struct{}, struct{ s interface{} }) error { return nil },
			wantErr: "error in dependency struct argument 2: field s must be exported",
		},
		"dependency struct field is not an interface": {
			handler: func(context.Context, *struct{}, struct{ S int }) error { return nil },
			wantErr: "error in dependency struct argument 2: field S must be an interface, got int",
		},
		"no return values": {
			handler: func(context.Context, *struct{}, interface{}) {},
			wantErr: "handler must have one return value, got 0",
		},
		"too many return values": {
			handler: func(context.Context, *struct{}, interface{}) (interface{}, error) { return nil, nil },
			wantErr: "handler must have one return value, got 2",
		},
		"return value is not an error": {
			handler: func(context.Context, *struct{}, interface{}) int { return 0 },
			wantErr: "handler's return type must be error, got int",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			handlerType := reflect.TypeOf(tt.handler)
			err := validateHandlerSignature(handlerType)
			if tt.wantOk {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestValidateListenerSignature(t *testing.T) {
	tests := map[string]struct {
		listener interface{}
		wantErr  string
		wantOk   bool
	}{
		"valid listener": {
			listener: func(context.Context, struct{}, interface{}) {},
			wantOk:   true,
		},
		"not a function": {
			listener: 0,
			wantErr:  "handler must be a function, got int",
		},
		"not enough arguments": {
			listener: func(context.Context) {},
			wantErr:  "handler must have at least 2 arguments, got 1",
		},
		"first argument is not a not context": {
			listener: func(int, struct{}, interface{}) {},
			wantErr:  "handler's first argument must be context.Context, got int",
		},
		"second argument is not a struct": {
			listener: func(context.Context, int, interface{}) {},
			wantErr:  "handler's second argument must be a struct, got int",
		},
		"third argument is not an interface": {
			listener: func(context.Context, struct{}, int) {},
			wantErr:  "argument 2 must be an interface, struct or *van.Van, got int",
		},
		"dependency struct field is not exported": {
			listener: func(context.Context, struct{}, struct{ s interface{} }) {},
			wantErr:  "error in dependency struct argument 2: field s must be exported",
		},
		"dependency struct field is not an interface": {
			listener: func(context.Context, struct{}, struct{ S int }) {},
			wantErr:  "error in dependency struct argument 2: field S must be an interface, got int",
		},
		"too many return values": {
			listener: func(context.Context, struct{}, interface{}) int { return 0 },
			wantErr:  "event handler should not have any return values",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			listenerType := reflect.TypeOf(tt.listener)
			err := validateListenerSignature(listenerType)
			if tt.wantOk {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestValidateExecLambdaSignature(t *testing.T) {
	tests := map[string]struct {
		fn      interface{}
		wantErr string
		wantOk  bool
	}{
		"valid": {
			fn:     func(dep1 interface{}, dep2 struct{ S interface{} }) error { return nil },
			wantOk: true,
		},
		"not a function": {
			fn:      1,
			wantErr: "function must be a function, got int",
		},
		"no return values": {
			fn:      func() {},
			wantErr: "function must have one return value, got 0",
		},
		"too many return values": {
			fn:      func() (int, error) { return 0, nil },
			wantErr: "function must have one return value, got 2",
		},
		"return value is not an error": {
			fn:      func() int { return 0 },
			wantErr: "return value must be an error, got int",
		},
		"dependency is not an interface": {
			fn:      func(int) error { return nil },
			wantErr: "argument 0 must be an interface, struct or *van.Van, got int",
		},
		"dependency struct field is not exported": {
			fn:      func(struct{ s interface{} }) error { return nil },
			wantErr: "error in dependency struct argument 0: field s must be exported",
		},
		"dependency struct field is not an interface": {
			fn:      func(struct{ S int }) error { return nil },
			wantErr: "error in dependency struct argument 0: field S must be an interface, got int",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fnType := reflect.TypeOf(tt.fn)
			err := validateExecLambdaSignature(fnType)

			if tt.wantOk {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}
