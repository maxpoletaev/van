package van_test

import (
	"context"
	"fmt"

	"github.com/maxpoletaev/van"
)

type SayHelloCommand struct {
	Name string
}

func SayHello(ctx context.Context, cmd *SayHelloCommand, bus van.Van) error {
	fmt.Printf("Hello, %s!\n", cmd.Name)
	done, _ := bus.Publish(ctx, HelloBeenSaidEvent{Name: cmd.Name, Timestamp: 1})
	<-done // wait for the event to be processed not to exit prematurely
	return nil
}

type HelloBeenSaidEvent struct {
	Name      string
	Timestamp int64
}

func HelloBeenSaid(ctx context.Context, event HelloBeenSaidEvent) error {
	fmt.Printf("Hello has been said to %s at %d", event.Name, event.Timestamp)
	return nil
}

func ExampleVan() {
	bus := van.New()
	bus.Handle(SayHelloCommand{}, SayHello)
	bus.Subscribe(HelloBeenSaidEvent{}, HelloBeenSaid)
	bus.Invoke(context.Background(), &SayHelloCommand{"Golang"})

	// Output:
	// Hello, Golang!
	// Hello has been said to Golang at 1
}
