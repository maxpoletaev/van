package van

import (
	"context"
	"math"
	"reflect"
	"testing"
)

type benchService interface {
	Run() int
}
type serviceA benchService
type serviceB benchService
type serviceC benchService
type serviceD benchService
type serviceE benchService
type serviceImpl struct {
	ret int
}
type benchCommand struct {
	val int
}

func (s *serviceImpl) Run() int { return s.ret }

func BenchmarkInvoke(b *testing.B) {
	bus := New()
	bus.Provide(func() (serviceA, error) {
		return &serviceImpl{ret: 1}, nil
	})
	bus.Provide(func(a serviceA) (serviceB, error) {
		return &serviceImpl{ret: 2}, nil
	})
	bus.Provide(func(a serviceA, b serviceB) (serviceC, error) {
		return &serviceImpl{ret: 3}, nil
	})
	bus.Provide(func(a serviceA, b serviceB, c serviceC) (serviceD, error) {
		return &serviceImpl{ret: 4}, nil
	})
	bus.Provide(func(a serviceA, b serviceB, c serviceC, d serviceD) (serviceE, error) {
		return &serviceImpl{ret: 5}, nil
	})
	bus.Handle(benchCommand{}, func(ctx context.Context, cmd *benchCommand, a serviceA, b serviceB, c serviceC, d serviceD, e serviceE) error {
		return nil
	})

	ctx := context.Background()
	cmd := &benchCommand{val: 1}
	var err error

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = bus.Invoke(ctx, cmd)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInvoke_SingleProvider(b *testing.B) {
	bus := New()
	bus.Provide(func() (serviceA, error) {
		return &serviceImpl{ret: 1}, nil
	})
	bus.Handle(benchCommand{}, func(ctx context.Context, cmd *benchCommand, a serviceA) error {
		return nil
	})

	ctx := context.Background()
	var err error
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err = bus.Invoke(ctx, &benchCommand{val: i})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInvoke_Singletons(b *testing.B) {
	bus := New()
	bus.ProvideSingleton(func() (serviceA, error) {
		return &serviceImpl{ret: 1}, nil
	})
	bus.ProvideSingleton(func(a serviceA) (serviceB, error) {
		return &serviceImpl{ret: 2}, nil
	})
	bus.ProvideSingleton(func(a serviceA, b serviceB) (serviceC, error) {
		return &serviceImpl{ret: 3}, nil
	})
	bus.ProvideSingleton(func(a serviceA, b serviceB, c serviceC) (serviceD, error) {
		return &serviceImpl{ret: 4}, nil
	})
	bus.ProvideSingleton(func(a serviceA, b serviceB, c serviceC, d serviceD) (serviceE, error) {
		return &serviceImpl{ret: 5}, nil
	})
	bus.Handle(benchCommand{}, func(ctx context.Context, cmd *benchCommand, a serviceA, b serviceB, c serviceC, d serviceD, e serviceE) error {
		return nil
	})

	ctx := context.Background()
	var err error

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = bus.Invoke(ctx, &benchCommand{val: i})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExec_SingleProvider(b *testing.B) {
	bus := New()
	bus.Provide(func() (serviceA, error) {
		return &serviceImpl{ret: 1}, nil
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := bus.Exec(context.Background(), func(a serviceA) error {
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExec_Bus(b *testing.B) {
	bus := New()
	var err error

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = bus.Exec(context.Background(), func(b Van) error {
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFuncCallStatic(b *testing.B) {
	for i := 0; i < b.N; i++ {
		math.Sqrt(float64(100000))
	}
}

func BenchmarkFuncCallReflection(b *testing.B) {
	sqrt := reflect.ValueOf(math.Sqrt)
	args := []reflect.Value{reflect.ValueOf(float64(100000))}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sqrt.Call(args)
	}
}
