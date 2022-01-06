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
	bus.HandleCommand(benchCommand{}, func(ctx context.Context, cmd *benchCommand, a serviceA, b serviceB, c serviceC, d serviceD, e serviceE) error {
		return nil
	})

	ctx := context.Background()
	var err error

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = bus.InvokeCommand(ctx, &benchCommand{val: i})
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
	bus.HandleCommand(benchCommand{}, func(ctx context.Context, cmd *benchCommand, a serviceA, b serviceB, c serviceC, d serviceD, e serviceE) error {
		return nil
	})

	ctx := context.Background()
	var err error

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = bus.InvokeCommand(ctx, &benchCommand{val: i})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolve_Bus(b *testing.B) {
	bus := New()
	var err error

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = bus.Resolve(func(b Van) error {
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
