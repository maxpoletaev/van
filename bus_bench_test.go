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
	bus.Provide(func() serviceA { return &serviceImpl{ret: 1} })
	bus.Provide(func(a serviceA) serviceB { return &serviceImpl{ret: 2} })
	bus.Provide(func(a serviceA, b serviceB) serviceC { return &serviceImpl{ret: 3} })
	bus.Provide(func(a serviceA, b serviceB, c serviceC) serviceD { return &serviceImpl{ret: 4} })
	bus.Provide(func(a serviceA, b serviceB, c serviceC, d serviceD) serviceE { return &serviceImpl{ret: 5} })
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
	bus.ProvideSingleton(func() serviceA { return &serviceImpl{ret: 1} })
	bus.ProvideSingleton(func(a serviceA) serviceB { return &serviceImpl{ret: 2} })
	bus.ProvideSingleton(func(a serviceA, b serviceB) serviceC { return &serviceImpl{ret: 3} })
	bus.ProvideSingleton(func(a serviceA, b serviceB, c serviceC) serviceD { return &serviceImpl{ret: 4} })
	bus.ProvideSingleton(func(a serviceA, b serviceB, c serviceC, d serviceD) serviceE { return &serviceImpl{ret: 5} })
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
	sqrt := func(v float64) float64 {
		return math.Sqrt(v)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sqrt(float64(i))
	}
}

func BenchmarkFuncCallReflection(b *testing.B) {
	sqrt := func(v float64) error {
		math.Sqrt(v)
		return nil
	}
	sqrtV := reflect.ValueOf(sqrt)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sqrtV.Call([]reflect.Value{reflect.ValueOf(float64(i))})
	}
}
