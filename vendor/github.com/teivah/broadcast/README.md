# broadcast

![CI](https://github.com/teivah/broadcast/actions/workflows/ci.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/teivah/broadcast)](https://goreportcard.com/report/github.com/teivah/broadcast)

Notification broadcaster in Go

## What?

`broadcast` is a library that allows sending repeated notifications to multiple goroutines with guaranteed delivery and user defined types.

## Why?

### Why not Channels?

The standard way to handle notifications is via a `chan struct{}`. However, sending a message to a channel is received by a single goroutine. 

The only operation that is broadcast to multiple goroutines is a channel closure. Yet, if the channel is closed, there's no way to send a message again.

❌ Repeated notifications to multiple goroutines

✅ Guaranteed delivery

### Why not sync.Cond?

`sync.Cond` is the standard solution based on condition variables to set up containers of goroutines waiting for a specific condition.

There's one caveat to keep in mind, though: the `Broadcast()` method doesn't guarantee that a goroutine will receive the notification. Indeed, the notification will be lost if the listener goroutine isn't waiting on the `Wait()` method.

✅ Repeated notifications to multiple goroutines

❌ Guaranteed delivery

## How?

### Step by Step

First, we need to create a `Relay` for a message type (empty struct in this case):

```go
relay := broadcast.NewRelay[struct{}]()
```

Once a `Relay` is created, we can create a new listener using the `Listener` method. As the `broadcast` library relies internally on channels, it accepts a capacity:

````go
list := relay.Listener(1) // Create a new listener based on a channel with a one capacity
````

A `Relay` can send a notification in three different manners:
* `Notify`: block until a notification is sent to all the listeners
* `NotifyCtx`: send a notification to all listeners unless the provided context times out or is canceled
* `Broadcast`: send a notification to all listeners in a non-blocking manner; delivery isn't guaranteed

On the `Listener` side, we can access the internal channel using `Ch`:

```go
<-list.Ch() // Wait on a notification
```

We can close a `Listener` and a `Relay` using `Close`:

```go
list.Close() 
relay.Close()
```

Closing a `Relay` and `Listener`s can be done concurrently in a safe manner.

### Example

```go
type msg string
const (
    msgA msg = "A"
    msgB     = "B"
    msgC     = "C"
)

relay := broadcast.NewRelay[msg]() // Create a relay for msg values
defer relay.Close()

// Listener goroutines
for i := 0; i < 2; i++ {
    go func(i int) {
        l := relay.Listener(1)  // Create a listener with a buffer capacity of 1
        for n := range l.Ch() { // Ranges over notifications
            fmt.Printf("listener %d has received a notification: %v\n", i, n)
        }
    }(i)
}

// Notifiers
time.Sleep(time.Second)
relay.Notify(msgA)                                     // Send notification with guaranteed delivery
ctx, _ := context.WithTimeout(context.Background(), 0) // Context with immediate timeout
relay.NotifyCtx(ctx, msgB)                             // Send notification respecting context cancellation
time.Sleep(time.Second)                                // Allow time for previous messages to be processed
relay.Broadcast(msgC)                                  // Send notification without guaranteed delivery
time.Sleep(time.Second)                                // Allow time for previous messages to be processed
```