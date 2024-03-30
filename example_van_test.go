package van_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/maxpoletaev/van"
)

type InMemoryCounter struct {
	value int
}

func (c *InMemoryCounter) Increment() int {
	c.value++
	return c.value
}

func (c *InMemoryCounter) Value() int {
	return c.value
}

type Counter interface {
	Value() int
	Increment() int
}

// ProvideCounter creates a counter instance
func ProvideCounter() (Counter, error) {
	return &InMemoryCounter{}, nil
}

type IncrementCommand struct {
	Value uint32
}

// Increment is a handler that processes IncrementCommand
func Increment(ctx context.Context, cmd *IncrementCommand, counter Counter, bus *van.Van) error {
	oldValue := counter.Value()
	newValue := counter.Increment()

	_ = bus.Publish(
		CounterUpdatedEvent{
			Timestamp: time.Now().Unix(),
			OldValue:  oldValue,
			NewValue:  newValue,
		},
	)

	return nil
}

// CounterUpdatedEvent is published whenever the counter is updated
type CounterUpdatedEvent struct {
	Timestamp int64
	OldValue  int
	NewValue  int
}

// CounterUpdated handles CounterUpdatedEvent
func CounterUpdated(ctx context.Context, evt CounterUpdatedEvent) {
	fmt.Printf("counter updated: %d -> %d\n", evt.OldValue, evt.NewValue)
}

func ExampleVan() {
	bus := van.New()

	bus.ProvideOnce(ProvideCounter)
	bus.Handle(IncrementCommand{}, Increment)
	bus.Subscribe(CounterUpdatedEvent{}, CounterUpdated)

	ctx := context.Background()
	if err := bus.Invoke(ctx, &IncrementCommand{}); err != nil {
		log.Fatalf("failed to call IncrementCommand: %v", err)
	}

	// wait for the events to be processed before exit
	bus.Wait()
}
