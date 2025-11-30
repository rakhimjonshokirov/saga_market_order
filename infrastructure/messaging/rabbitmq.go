package messaging

import (
	"context"
	"fmt"
	"log"

	"github.com/rabbitmq/amqp091-go"
)

// RabbitMQ provides message bus functionality
type RabbitMQ struct {
	conn    *amqp091.Connection
	channel *amqp091.Channel
	url     string
}

// EventHandler is a function that processes event data
type EventHandler func(ctx context.Context, eventData []byte) error

func NewRabbitMQ(url string) *RabbitMQ {
	return &RabbitMQ{url: url}
}

// Connect establishes connection to RabbitMQ
func (r *RabbitMQ) Connect() error {
	conn, err := amqp091.Dial(r.url)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	r.conn = conn
	r.channel = ch

	// Declare exchange for events
	err = ch.ExchangeDeclare(
		"events", // name
		"topic",  // type
		true,     // durable
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	log.Println("✅ Connected to RabbitMQ")
	return nil
}

// Publish publishes an event to RabbitMQ
func (r *RabbitMQ) Publish(eventType string, eventData []byte) error {
	if r.channel == nil {
		return fmt.Errorf("RabbitMQ channel not initialized")
	}

	// Routing key = event type (e.g., "OrderAccepted", "SwapExecuted")
	routingKey := eventType

	err := r.channel.PublishWithContext(
		context.Background(),
		"events",   // exchange
		routingKey, // routing key
		false,      // mandatory
		false,      // immediate
		amqp091.Publishing{
			ContentType:  "application/json",
			Body:         eventData,
			DeliveryMode: amqp091.Persistent, // Persistent messages
		},
	)

	if err != nil {
		return fmt.Errorf("failed to publish event %s: %w", eventType, err)
	}

	log.Printf("📤 Published event: %s", eventType)
	return nil
}

// Subscribe subscribes to events and processes them with the handler
func (r *RabbitMQ) Subscribe(eventType string, handler EventHandler) error {
	if r.channel == nil {
		return fmt.Errorf("RabbitMQ channel not initialized")
	}

	// Create queue for this event type
	queueName := fmt.Sprintf("queue.%s", eventType)

	queue, err := r.channel.QueueDeclare(
		queueName, // name
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// Bind queue to exchange with routing key = event type
	err = r.channel.QueueBind(
		queue.Name, // queue name
		eventType,  // routing key
		"events",   // exchange
		false,      // no-wait
		nil,        // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue: %w", err)
	}

	// Start consuming
	msgs, err := r.channel.Consume(
		queue.Name, // queue
		"",         // consumer tag
		false,      // auto-ack (manual ack for reliability)
		false,      // exclusive
		false,      // no-local
		false,      // no-wait
		nil,        // args
	)
	if err != nil {
		return fmt.Errorf("failed to consume: %w", err)
	}

	// Process messages in goroutine
	go func() {
		log.Printf("👂 Subscribed to event: %s (queue: %s)", eventType, queueName)

		for msg := range msgs {
			ctx := context.Background()

			log.Printf("📥 Received event: %s", eventType)

			// Process event with handler
			err := handler(ctx, msg.Body)

			if err != nil {
				log.Printf("❌ Failed to process event %s: %v", eventType, err)
				// NACK - requeue message for retry
				msg.Nack(false, true)
			} else {
				log.Printf("✅ Successfully processed event: %s", eventType)
				// ACK - acknowledge successful processing
				msg.Ack(false)
			}
		}
	}()

	return nil
}

// Close closes the RabbitMQ connection
func (r *RabbitMQ) Close() error {
	if r.channel != nil {
		r.channel.Close()
	}
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}
