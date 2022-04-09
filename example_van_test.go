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

	err := bus.Publish(ctx, HelloBeenSaidEvent{Name: cmd.Name, Timestamp: 1})
	if err != nil {
		return err
	}

	return nil
}

type HelloBeenSaidEvent struct {
	Name      string
	Timestamp int64
}

func HelloBeenSaid(ctx context.Context, event HelloBeenSaidEvent) {
	fmt.Printf("Hello has been said to %s at %d", event.Name, event.Timestamp)
}

func ExampleVan() {
	bus := van.New()
	bus.Handle(SayHelloCommand{}, SayHello)
	bus.Subscribe(HelloBeenSaidEvent{}, HelloBeenSaid)

	err := bus.Invoke(context.Background(), &SayHelloCommand{"Golang"})
	if err != nil {
		panic(err)
	}

	// wait for the events to be processed before exit
	bus.Wait()

	// Output:
	// Hello, Golang!
	// Hello has been said to Golang at 1
}
