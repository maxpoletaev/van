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
// define command struct
type PrintHelloCommand struct {
	Name string
}

// register command handler
func PrintHello(ctx context.Context, cmd *PrintHelloCommand) error {
	log.Printf("Hello, %s!", cmd.Name)
}
bus.Handle(PrintHelloCommand{}, PrintHello)

// send command to the bus
ctx := context.Background()
cmd := &PrintHelloCommand{Name: "Harry"}
if err := bus.Invoke(ctx, cmd); err != nil {
	log.Printf("[ERROR] failed to print hello: %v", err)
}
```

## Events

 * Event is a broadcast message informing that something has happened.
 * Events are simple DTO objects without behaviour.
 * Events are processed asynchronously.
 * Events are immutable and cannot be modified by listeners.
 * Each event may have a zero to infinity number of listeners.

```go
// define event struct
type OrderCreatedEvent struct {
	OrderID	int
	Ts      int64
}

// register event listener
func OrderCreated(ctx *context.Context, event OrderCreatedEvent) {
	log.Printf("[INFO] order %d created at %d", event.OrderID, event.Ts)
}
bus.Subscribe(OrderCreatedEvent{}, OrderCreated)

// publish event to the bus
event := OrderCreatedEvent{
	OrderID: 134,
	Ts:      time.Now().Unix(),
}
ctx := context.Background()
if err := bus.Publish(ctx, event); err != nil {
	log.Printf("[ERROR] failed to publishing an event: %v", err)
}
```

## Providers

 * Provider is essentially a constructor of an arbitrary type.
 * Provider should return an interface and an error.
 * Providers can depend on other providers.
 * Providers can be either transitive (executed every time the dependency is requested), or signletons.
 * There is no such thing as "optional dependency", provider must return an error if it can’t provide one.

```go
type Logger interface {
	Printf(string, ...interface{})
}

// singleton provider is guaranteed to be executed not more than once
bus.ProvideSingleton(func() (Logger, error) {
	flags := log.LstdFlags | log.LUTC | log.Lshortfile
	return log.New(os.Stdout, "", flags)
})

// transitive provider is executed every time the dependency is requested
// here we initialize new database connection every time UserRepo is used as a dependency
func newUserRepoProvider(db *sql.DB) interface{} {
	return func(ctx context.Context, logger Logger) (UserRepo, error) {
		conn := db.Conn(ctx) // TODO: make sure context is cancelled
		return newPostgresUserRepo(conn, logger)
	}
}
db := sql.Open("postgres", "...")
bus.Provide(newUserRepoProvider(db))
```

## Handlers

 * Handler is a function associated with a command or an event.
 * Handlers take at least two arguments: context and command/event struct.
 * Handlers may have dependencies provided in extra arguments as interfaces.
 * Command handler can return an error which will propagated to the caller as is.
 * Event handlers cannot return any values, and there is no way to propagate the
   error to the caller. Error handling should be done in place and logged if necessary.

```go
func PrintHelloWorld(ctx context.Context, cmd *PrintHelloWorldCommand, logger Logger, bus van.Van) error {
	logger.Print("Hello, World!")
	bus.Publish(ctx, HelloWorldPrintedEvent{})
	return nil
}

func HelloWorldPrinted(ctx context.Context, event HelloWorldPrintedEvent, logger Logger) {
	logger.Print("hello world has been printed")
}
```

## Is it slow?

Well, yeah... Although it tries to do most of the checks during the start up, it’s still slow as hell due to reflection magic under the hood used for dynamically-constructed function arguments, the most painful of which is `reflect.Value.Call()`.

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

The key is to use singletons whenever possible to avoid cascade `reflect.Value.Call()` calls. This is far from ideal but ten times faster:

```
BenchmarkInvoke-12                	   82945	     13932 ns/op	    3640 B/op	     145 allocs/op
BenchmarkInvoke_Singletons-12     	  691203	      1729 ns/op	     352 B/op	      11 allocs/op
```

I mean, it is not extremely bad, given the fact that we are still in the microseconds scale. However, it is better to stay away if performance is the priority.

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

ctx := context.Background()
cmd := &SumCommand{A: 1, B: 2}
if err := bus.Invoke(ctx, cmd); err != nil {
	panic(err)
}

println(cmd.Result) // 3
```

## Can I have multiple providers for the same type?

Not really, but you can create a type alias:

```go
type Logger interface {
    Printf(string)
}

type ErrorLogger ErrorLogger

bus.ProvideSingleton(func() (Logger, error) {
	flags := log.LstdFlags | log.LUTC
	return log.New(os.Stdout, "", flags), nil
})

bus.ProvideSingleton(func() (ErrorLogger, error) {
	flags := log.LstdFlags | log.LUTC | log.Llongfile
	return logging.NewLogger(os.Stderr, "[ERROR] ", flags), nil
})
```
