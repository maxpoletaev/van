# 🚐 Van

(Yet another?) application-level reflection-based command/event bus with
automatic dependency-injection.

## Status

BETA. There still might be breaking API changes in future versions.

## Idea

Van introduces some variation of the CQS pattern, combined with automatic dependency
injection, making the application components low-coupled, highly testable and maintainable.

## Commands

 * Command is a signal to the application to perform some action.
 * Commands are simple DTO objects without behaviour.
 * Commands are processed with command handlers.
 * Each command can be associated with one handler.
 * Commands are processed synchronously (request-response).
 * Commands are mutable allowing handlers to set the return values.

```go
// Define command struct
type PrintHelloCommand struct {
	Name string
}

func (cmd *PrintHelloCommand) Handle(ctx *context.Context)

// Register command handler
func PrintHello(ctx *context.Context, cmd *PrintHelloCommand, logger Logger) error {
    logger.Printf("Hello, %s!", cmd.Name)
    return nil
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

```go
// define event struct
type OrderCreatedEvent struct {
	OrderID	int
	Ts      int64
}

// register event listener
bus.Subscribe(OrderCreatedEvent{}, func(ctx context.Context, event OrderCreatedEvent) {
    log.Printf("[INFO] order %d created at %d", event.OrderID, event.Ts)
})

// publish event to the bus
event := OrderCreatedEvent{
	OrderID: 134,
	Ts:      time.Now().Unix(),
}

if err := bus.Publish(event); err != nil {
	log.Printf("[ERROR] failed to publish OrderCreatedEvent: %v", err)
}
```

## Handlers

 * Handler is a function associated with a command or an event.
 * Handlers take at least two arguments: context and command/event struct.
 * Handlers may have dependencies provided in extra arguments as interfaces.
 * Command handler can return an error which will propagated to the caller as is.
 * Event handlers cannot return any values, nor can they propagate any state back
   to the caller, including errors. Therefore, there is no indication of whether
   the event has been processed successfully.
 * Command handlers are synchronous. Event handlers are executed in the background
   (the order of execution is not specified).

```go
func PrintHelloWorld(ctx context.Context, cmd *PrintHelloWorldCommand, logger Logger, bus van.Van) error {
	logger.Print("Hello, World!")
	if err := bus.Publish(HelloWorldPrintedEvent{}); err != nil {
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
 * Providers can be either regular constructors (executed every time the dependency
   is requested), or singletons.
 * Regular providers can depend on `context.Context`. Singleton providers cannot,
   nor can they depend on providers that use context as a dependency.
 * There is no such thing as "optional dependency", provider must return an error
   if it can’t provide one.

```go
type Logger interface {
	Printf(string, ...interface{})
}

// Singleton provider is guaranteed to be executed only once.
func ProvideLogger() (Logger, error) {
	flags := log.LstdFlags | log.LUTC | log.Lshortfile
	return log.New(os.Stdout, "", flags)
}

// Regular provider is executed every time the dependency is requested.
func newUserRepoProvider(db *sql.DB) van.ProviderFunc {
	return func(ctx context.Context, logger Logger) (UserRepo, error) {
		logger.Printf("initializing new user repository")
		return newPostgresUserRepo(db.Conn(ctx), logger)
	}
}

func main() {
   db := sql.Open("postgres", "...")

   bus.ProvideSingleton(ProvideLogger)

   bus.Provide(newUserRepoProvider(db))
}
```

## Dependency Injection

Each command handler, event handler, or provider may have an arbitrary number of
dependencies defined as the function arguments. The dependency must be of an interface
type and there should be a registered dependency provider for the given type.

```go
func SayHello(ctx context.Context, cmd *SayHelloCommand, logger Logger, bus van.Van) error {
	logger.Print("Hello, World!")
	bus.Publish(HelloWorldPrintedEvent{})
	return nil
}
```

In case one has too many dependencies to be passed as function arguments, it is
possible to pack them into a struct. Each field of that struct still needs to be
of an interface type. You can combine any number of such structs in the function
arguments.

```go
func DependencySet struct {
	Bus    *van.Van
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
BenchmarkFuncCall_StaticStack-12           	1000000000	         0.2446 ns/op	   0 B/op	       0 allocs/op
BenchmarkFuncCall_StaticHeap-12            	54928545	        20.68 ns/op	      16 B/op	       1 allocs/op
BenchmarkFuncCall_Reflection-12            	 4555992	       248.2 ns/op	      32 B/op	       2 allocs/op
```

If we compare a relatively large dependency graph constructed statically with
the one constructed dynamically using dependency injection, the difference will
be at about 40 to 50 times:

```
BenchmarkBus_LargeGraphTransitive-12    	   82728	     12986 ns/op	    2816 B/op	     128 allocs/op
BenchmarkNoBus_LargeGraph-12               	 3522093	       336.5 ns/op	     224 B/op	      28 allocs/op
```

A general recommendation is not to forget using singletons whenever possible
to reduce the number of dynamic reflection calls to the providers:

```
BenchmarkBus_LargeGraphTransitive-12    	   82728	     12986 ns/op	    2816 B/op	     128 allocs/op
BenchmarkBus_LargeGraphSingletons-12    	  756583	      1590 ns/op	     176 B/op	      10 allocs/op
```

Given the fact that we are still in the nanoseconds (10<sup>−9</sup> seconds)
scale, is unlikely to introduce any visible delay in 95% of the cases. Most
probably, your application will spend way more time doing actual business logic,
database round trips and JSON serialization.

So, the impact of the bus on the response time of a typical go service is
estimated at around 1% at worst.

## Type Safety

Even though there is a lot of reflection under the hood, Van tries to do most of
the type checking at the startup. That includes checking the types of the
dependencies and the return values of the providers. It does not, however, 
prevent you from passing a wrong type to the `Invoke` method, meaning that you 
can still get a type error in run time (not panicking, though).

```go

## Return Values

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

cmd := &SumCommand{
	A: 1,
	B: 2,
}

if err := bus.Invoke(context.TODO(), cmd); err != nil {
	panic(err)
}

fmt.Println(cmd.Result) // 3
```

## Multiple Providers for the Same Type

You can achieve this by defining a new type for the same interface:

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
