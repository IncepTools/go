package events_test

import (
	"context"
	"fmt"

	"github.com/IncepTools/go/src/platform/events"
)

// OrderPlaced is published by checkout. Nothing in checkout imports the
// audit log, the mailer or the usage meter.
type OrderPlaced struct {
	OrderID string
	Total   int64
}

// Subscribers are typed: no `any` at the call site and no assertion in the
// handler.
func Example() {
	bus := events.New()

	events.On(bus, "orders.audit", events.Sync,
		func(_ context.Context, e OrderPlaced) error {
			fmt.Println("audit:", e.OrderID)
			return nil
		})
	events.On(bus, "orders.meter", events.Sync,
		func(_ context.Context, e OrderPlaced) error {
			fmt.Println("meter:", e.Total)
			return nil
		})

	if err := events.Publish(context.Background(), bus, OrderPlaced{
		OrderID: "9c1e", Total: 1200,
	}); err != nil {
		panic(err)
	}
	// Output:
	// audit: 9c1e
	// meter: 1200
}

// An async handler's failure never reaches the publisher: the side effect
// must not fail the request that caused it. The bus logs it instead.
func ExamplePublish_asyncFailuresDoNotReachThePublisher() {
	bus := events.New()
	events.On(bus, "orders.mail", events.Async,
		func(context.Context, OrderPlaced) error {
			return fmt.Errorf("the mail server refused the connection")
		})

	err := events.Publish(context.Background(), bus, OrderPlaced{OrderID: "9c1e"})
	bus.Wait()

	fmt.Println("publisher saw:", err)
	// Output: publisher saw: <nil>
}
