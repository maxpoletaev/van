# Van 🚐

(Yet another?) reflection-based in-app command/event bus implementation with dependency-injection. Heavily inspired by https://github.com/uber-go/dig and https://watermill.io/docs/cqrs/.

## Status

ALPHA. There might be breaking API changes in future versions.

## Commands

 * Command is a signal to the application to perform some action.
 * Commands are simple DTO objects without behaviour.
 * Commands are processed with command handlers.
 * Each command can be associated with only one handler.
 * Commands are processed synchronously (request-response).
 * Commands are mutable. Handlers can modify command structs to provide return values.

```go
type PrintHelloCommand struct {
	Name string
}

func PrintHello(ctx context.Context, cmd *PrintHelloCommand) error {
	fmt.Printf("Hello, %s!\n", cmd.Name)
}

// Register handler
bus.HandleCommand(PrintHelloCommand{}, PrintHello)

// Send command to the bus
cmd := &PrintHelloCommand{Name: "Harry"}
err := bus.InvokeCommand(context.Background(), cmd)
if err != nil {
	panic(err)
}
```

## Events

 * Event is a broadcast message informing that something has happened.
 * Events are simple DTO objects without behaviour.
 * Events are processed asynchronously.
 * Events are immutable and cannot have return values.
 * Each event may have a zero to infinity number of listeners.
 * A failing listener doesn’t prevent other event listeners from processing the event.

```go
type OrderCreatedEvent struct {
	OrderID	  string
	Timestamp int64
}

func OrderCreated(ctx *context.Context, event OrderCreatedEvent) error {
	fmt.Printf("order created: %d", event.OrderID)
	return nil
}

// Subscribe to event
bus.ListenEvent(OrderCreatedEvent{}, OrderCreated)

// Send event to the bus
event := OrderCreatedEvent{
	OrderID:   "ord-134",
	Timestamp: time.Now().Unix(),
}
bus.EmitEvent(context.Background(), event)
```

Although event processing is asynchronous, there are done and error channels that you can use to wait for the event to be processed, therefore converting it to a synchronous call:

```go
done, errchan := bus.EmitEvent(ctx, event)
select {
case <-done:
	return nil
case err := <-errchan:
	return err
}
```

## Providers

 * Provider is essentially a constructor of an arbitrary type.
 * Providers can depend on other providers.
 * Proviers are executed every time the dependecy is requested, they are not singletons.

```go
bus.Provide(func() Logger {
	return &logging.DumbStdoutLogger{}
})

bus.Provide(func(logger Logger) UserRepository {
	return &PersistentUserRepository{Logger: logger}
})
```


## Handlers

 * Handler is a function associated with a command or an event.
 * Handlers should have at least two arguments: context and command/event struct.
 * Handlers may have dependencies provided in extra arguments.
 * Handlers cannot return values, except for error.

```go
commandHandler := func(ctx context.Context, cmd *Command, logger Logger) error {
	logger.Print("Hello, World!")
	return
}
eventHandler := func(ctx context.Context, event Event) error {
	return
}
```

## Is it slow?

Well, yeah... Although it tries to do most of the checks during the handler registration, it’s still slow as hell due to reflection magic under the hood used for dynamically-constructed function arguments, the most painful of which is `reflect.Value.Call()`

The following benchmark shows that simple dynamic function calls in Go are about 1000 times slower than static function calls, and this is even without the dependency-injection overhead involved.

```
goos: darwin
goarch: amd64
pkg: github.com/maxpoletaev/van
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkFuncCallStatic-12        	1000000000	   0.2447 ns/op	       0 B/op	       0 allocs/op
BenchmarkFuncCallReflection-12    	5328840	       222.5 ns/op	      32 B/op	       2 allocs/op
```

<details>
<summary>Benchmark code</summary>

```go
func BenchmarkFuncCallStatic(b *testing.B) {
	for i := 0; i < b.N; i++ {
		math.Sqrt(float64(100000))
	}
}

func BenchmarkFuncCallReflection(b *testing.B) {
	args := []reflect.Value{reflect.ValueOf(float64(100000))}
	sqrt := reflect.ValueOf(math.Sqrt)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sqrt.Call(args)
	}
}

```
</details>

The key is to use singletons whenever possible to avoid `reflect.Value.Call()`:

```
BenchmarkInvoke-12                	   82945	     13932 ns/op	    3640 B/op	     145 allocs/op
BenchmarkInvoke_Singletons-12     	  691203	      1729 ns/op	     352 B/op	      11 allocs/op
```

I mean, it is not extremely bad, given the fact that Go still uses lots of dynamic function calls in its standard library (e.g. `template/html`). However, it is better to stay away if performance is the priority. This project is more about convenience over performance.

## How do I return a value from a command handler?

There are no return values, but this can be handled with the command type itself:

```go
type SumCommand struct {
	A      int
	B      int
	Result int
}

func Sum(ctx context.Context, cmd *SumCommand) error {
	cmd.Result = cmd.A + cmd.B
	return nil
}

cmd := &SumCommand{A: 1, B: 2}
err := bus.InvokeCommand(context.Background(), cmd)
if err != nil {
	panic(err)
}

println(cmd.Result) // 3
```

## How do I create a parametrized provider?

Create a wrapper function for your provider. No need to specify the exact return type for it:

```go
func newLoggerProvider(logLevel string) interface{} {
	return func() Logger {
		return &SimpleLogger{Level: logLevel}
	}
}
bus.Provide(newLoggerProvider("INFO"))
```

## How do I create a singleton provider?

Initialize it outside of the provider function:

```go
authClient := auth.NewClient()
bus.Provide(func() AuthClient {
    return authClient
})
```

## Can I have multiple providers for the same type?

Not really, but you can create a type alias:

```go
type Logger interface {
    Print(string)
}
type Logger2 Logger

bus.Provide(func() Logger { logging.NewLogger() })
bus.Provide(func() Logger2 { logging.NewLogger() })
```

## How do I access the bus inside a handler?

There’s a provider already registered for the Van type:

```go
func CreateUser(ctx context.Context, cmd *CreateUserCommand, bus van.Van) error {
	// ...
	bus.EmitEvent(ctx, UserCreatedEvent{User: user})
	return nil
}
```
