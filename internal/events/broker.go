package events

import (
	"sync"
)

// Event represents an SSE event to be sent to clients
type Event struct {
	Type string // Event type (e.g., "machineStatus", "machineAdded", "userChanged")
	HTML string // HTML content to be sent in the SSE data field
}

// Broker manages SSE connections and broadcasts events to all connected clients
type Broker struct {
	clients    map[chan Event]bool
	mu         sync.RWMutex
	register   chan chan Event
	unregister chan chan Event
	broadcast  chan Event
	done       chan struct{}
}

// NewBroker creates and starts a new event broker
func NewBroker() *Broker {
	b := &Broker{
		clients:    make(map[chan Event]bool),
		register:   make(chan chan Event),
		unregister: make(chan chan Event),
		broadcast:  make(chan Event, 10), // Buffer up to 10 events
		done:       make(chan struct{}),
	}
	go b.run()
	return b
}

// run is the main event loop for the broker
func (b *Broker) run() {
	for {
		select {
		case client := <-b.register:
			b.mu.Lock()
			b.clients[client] = true
			b.mu.Unlock()

		case client := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[client]; ok {
				delete(b.clients, client)
				close(client)
			}
			b.mu.Unlock()

		case event := <-b.broadcast:
			b.mu.RLock()
			for client := range b.clients {
				select {
				case client <- event:
				default:
					// Client is slow or disconnected, skip
				}
			}
			b.mu.RUnlock()

		case <-b.done:
			// Cleanup: close all client channels
			b.mu.Lock()
			for client := range b.clients {
				close(client)
			}
			b.clients = make(map[chan Event]bool)
			b.mu.Unlock()
			return
		}
	}
}

// Subscribe creates a new client channel and registers it with the broker
func (b *Broker) Subscribe() chan Event {
	client := make(chan Event, 5) // Buffer a few events per client
	b.register <- client
	return client
}

// Unsubscribe removes a client channel from the broker
func (b *Broker) Unsubscribe(client chan Event) {
	b.unregister <- client
}

// Broadcast sends an event to all connected clients
func (b *Broker) Broadcast(event Event) {
	select {
	case b.broadcast <- event:
	default:
		// Broadcast channel is full, skip this event
	}
}

// ClientCount returns the number of currently connected clients
func (b *Broker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// Close shuts down the broker and closes all client connections
func (b *Broker) Close() {
	close(b.done)
}
