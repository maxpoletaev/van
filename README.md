# 🚐 Van

(Yet another?) reflection-based in-app command/event bus with dependency-injection.

## Status

BETA. There still might be breaking API changes in future versions.

## Commands

 * Command is a signal to the application to perform some action.
 * Commands are simple DTO objects without behaviour.
 * Commands are processed with command handlers.
 * Each command can be associated with only one handler.
 * Commands are processed synchronously (request-response).
 * There are no return values but handlers can modify the command struct to
   provide some state back to the caller.

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
	log.Printf("[ERROR] failed to invoke PrintHelloCommand: %v", err)
}
```

## Events

 * Event is a broadcast message informing that something has happened.
 * Events are simple DTO objects without behaviour.
 * Events are immutable and cannot be modified by listeners.
 * Each event may have zero to infinity number of listeners.
 * In the case of multiple listeners, they are executed concurrently, but not
   asynchronously. In other words, `bus.Publish()` runs each event handler in a
   separate goroutine but waits for them to finish the execution before returning.

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
	log.Printf("[ERROR] failed to publish OrderCreatedEvent: %v", err)
}
```

## Handlers

 * Handler is a function associated with a command or an event.
 * Handlers take at least two arguments: context and command/event struct.
 * Handlers may have dependencies provided in extra arguments as interfaces.
 * Command handler can return an error which will propagated to the caller as is.
 * Event handlers cannot return any values, nor can they propagate any state back
   to the caller, including errors. Therefore there is no indication of whether
   the event has been processed successfully.

```go
func PrintHelloWorld(ctx context.Context, cmd *PrintHelloWorldCommand, logger Logger, bus van.Van) error {
	logger.Print("Hello, World!")
	if err := bus.Publish(ctx, HelloWorldPrintedEvent{}); err != nil {
		logger.Printf("failed to publish an event: %v", err)
		return err
	}
	return nil
}

func HelloWorldPrinted(ctx context.Context, event HelloWorldPrintedEvent, logger Logger) {
	logger.Print("hello world has been printed")
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
		conn := db.Conn(ctx) // TODO: make sure the context is cancelled at the end of the request lifespan
		return newPostgresUserRepo(conn, logger)
	}
}
db := sql.Open("postgres", "...")
bus.Provide(newUserRepoProvider(db))
```

## Dependency Injection

Each command handler, event handler, or provider may have an arbitrary number of
dependencies defined as the function arguments. The dependency must be of an interface
type and there should be a registered dependency provider for the given type.

```go
func SayHello(ctx context.Context, cmd *SayHelloCommand, logger Logger, bus van.Van) error {
	logger.Print("Hello, World!")
	bus.Publish(ctx, HelloWorldPrintedEvent{})
	return nil
}
```

In case one has too many dependencies to be passed as function arguments, it is
possible to pack them into a struct. Each field of that struct still needs to be
of an interface type. You can combine any number of such structs in the function
arguments.


```go
func DependencySet struct {
	Bus    van.Van
	Logger Logger
}

func HelloWorldPrinted(ctx context.Context, event HelloWorldPrintedEvent, deps DependencySet) {
	deps.Logger.Print("hello world has been printed")
}
```

## Is it fast?

Although it tries to do most of the heavy lifting during the start-up, it’s still
considered to be slow compared to "native" code due to reflection magic under
the hood used for dynamically-constructed function arguments.

The following benchmark shows that simple dynamic function calls in Go can be 10
to 1000 times slower than static function calls, and this is even without the
dependency-injection overhead involved.

```
goos: darwin
goarch: amd64
pkg: github.com/maxpoletaev/van
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkFuncCallNative-12        	1000000000	         0.2464 ns/op	       0 B/op	       0 allocs/op
BenchmarkFuncCallNativeHeap-12    	  53380869	          20.92 ns/op	      16 B/op	       1 allocs/op
BenchmarkFuncCallReflection-12    	   4695636	          250.9 ns/op	      32 B/op	       2 allocs/op
```

<details>
<summary>Benchmark code</summary>

```go
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
```
</details>

A general optimization would be to use singleons whenever possible to reduce the
number of reflection calls to the providers:

```
BenchmarkInvoke_Transitive-12        	   80292	     13556 ns/op	    2816 B/op	     128 allocs/op
BenchmarkInvoke_Singletons-12        	  618997	      1766 ns/op	     176 B/op	      10 allocs/op
```

The overall picture is not extremely bad. Given the fact that we are still in
the nanoseconds (10<sub>−9<sub> seconds) scale, is unlikely to introduce any
visible delay in 95% of the cases. Most probably, your application will spend way
more time doing actual business logic, database round trips and JSON serialization.

So, the contribution of the bus to the response time of a typical go service is
estimated at around 1% at worst.

## Is it safe?

Reflection is often risky. Because there is no compile-time type checking, the
application may panic at run time if an invalid type sneaks in.

Van tries to minimize that risk by doing run-time type checking of the arguments
and the return values of the dependency graph at the startup. If something goes
wrong with the types, the app will crash right away, not after days of running
in prod.

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

type ErrorLogger Logger

bus.ProvideSingleton(func() (Logger, error) {
	flags := log.LstdFlags | log.LUTC
	return log.New(os.Stdout, "", flags), nil
})

bus.ProvideSingleton(func() (ErrorLogger, error) {
	flags := log.LstdFlags | log.LUTC | log.Llongfile
	return log.New(os.Stderr, "[ERROR] ", flags), nil
})
```

## Credits

The general idea and some code snippets are inspired by:

 * Go Dependency Injector by Uber - https://github.com/uber-go/dig
 * The CQS/CQRS Pattern - https://watermill.io/docs/cqrs/
