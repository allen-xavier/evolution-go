package broker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Handler func([]byte) error

// Run consumes the two global queues published by Evolution Go. It reconnects
// until the context is cancelled, so Swarm service restarts are not required for
// short RabbitMQ outages.
func Run(ctx context.Context, amqpURL string, logger *slog.Logger, handler Handler) error {
	if amqpURL == "" {
		return fmt.Errorf("AMQP_URL is required")
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := consume(ctx, amqpURL, logger, handler)
		if ctx.Err() != nil {
			return nil
		}
		logger.Error("rabbitmq consumer disconnected", "error", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func consume(ctx context.Context, amqpURL string, logger *slog.Logger, handler Handler) error {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return err
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()

	if err := channel.Qos(20, 0, false); err != nil {
		return err
	}

	deliveries := make(chan amqp.Delivery, 40)
	for _, queueName := range []string{"message", "sendmessage"} {
		queue, err := channel.QueueDeclare(
			queueName,
			true,
			false,
			false,
			false,
			amqp.Table{"x-queue-type": "quorum", "x-ha-policy": "all"},
		)
		if err != nil {
			return fmt.Errorf("declare queue %s: %w", queueName, err)
		}
		stream, err := channel.Consume(queue.Name, "chatwoot-connector-"+queueName, false, false, false, false, nil)
		if err != nil {
			return fmt.Errorf("consume queue %s: %w", queueName, err)
		}
		go func() {
			for delivery := range stream {
				select {
				case deliveries <- delivery:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	logger.Info("rabbitmq consumer connected", "queues", "message,sendmessage")
	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-closed:
			if err == nil {
				return fmt.Errorf("rabbitmq connection closed")
			}
			return err
		case delivery := <-deliveries:
			if err := handler(delivery.Body); err != nil {
				logger.Error("invalid evolution event", "error", err)
				_ = delivery.Nack(false, false)
				continue
			}
			_ = delivery.Ack(false)
		}
	}
}
