package van

import (
	"context"
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

func BenchmarkInvoke_Transitive(b *testing.B) {
	bus := New()
	ctx := context.Background()
	cmd := &benchCommand{val: 1}

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

	var err error

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err = bus.Invoke(ctx, cmd)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInvoke_Singletons(b *testing.B) {
	ctx := context.Background()
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

	var err error

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err = bus.Invoke(ctx, &benchCommand{val: i})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInvoke_SingleProvider(b *testing.B) {
	ctx := context.Background()
	bus := New()

	bus.Provide(func() (serviceA, error) {
		return &serviceImpl{ret: 1}, nil
	})
	bus.Handle(benchCommand{}, func(ctx context.Context, cmd *benchCommand, a serviceA) error {
		return nil
	})

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

	for i := 0; i < b.N; i++ {
		err = bus.Exec(context.Background(), func(b Van) error {
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func div(a, b float64) float64 {
	return a / b
}

func BenchmarkFuncCallNative(b *testing.B) {
	for i := 0; i < b.N; i++ {
		div(float64(987654.321), float64(123456.789))
	}
}

func BenchmarkFuncCallNativeHeap(b *testing.B) {
	for i := 0; i < b.N; i++ {
		// make a heap allocation in each iteration to simulate
		// the behaviour similar to the reflection call
		args := make([]float64, 0)
		args = append(args, float64(987654.321), float64(123456.789))
		div(args[0], args[1])
	}
}

func BenchmarkFuncCallReflection(b *testing.B) {
	args := []reflect.Value{
		reflect.ValueOf(float64(987654.321)),
		reflect.ValueOf(float64(123456.789)),
	}
	divfn := reflect.ValueOf(div)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		divfn.Call(args)
	}
}
