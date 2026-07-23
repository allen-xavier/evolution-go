package broker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Handler func([]byte) error

const (
	consumerWorkers                 = 4
	maxProcessingAttempts           = 8
	retryDelay                      = 15 * time.Second
	publishConfirmTimeout           = 10 * time.Second
	legacyAliasColumnError          = `column "alias_jid" does not exist`
	legacyCanonicalAliasColumnError = "column excluded.canonical_jid does not exist"
)

type queuedDelivery struct {
	queueName string
	delivery  amqp.Delivery
}

type processingResult struct {
	item queuedDelivery
	err  error
}

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
	if err := channel.Confirm(false); err != nil {
		return fmt.Errorf("enable rabbitmq publisher confirms: %w", err)
	}
	publishConfirmations := channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	deliveries := make(chan queuedDelivery, 40)
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
		if err := declareReliabilityQueues(channel, queueName); err != nil {
			return err
		}
		stream, err := channel.Consume(queue.Name, "chatwoot-connector-"+queueName, false, false, false, false, nil)
		if err != nil {
			return fmt.Errorf("consume queue %s: %w", queueName, err)
		}
		go func(sourceQueue string) {
			for delivery := range stream {
				select {
				case deliveries <- queuedDelivery{queueName: sourceQueue, delivery: delivery}:
				case <-ctx.Done():
					return
				}
			}
		}(queueName)
	}

	if recovered, err := replayRecoverableDeadLetters(ctx, channel, publishConfirmations); err != nil {
		logger.Error("failed to replay recoverable dead-letter events", "error", err)
	} else if recovered > 0 {
		logger.Info("replayed recoverable dead-letter events", "count", recovered)
	}

	logger.Info(
		"rabbitmq consumer connected",
		"queues",
		"message,sendmessage",
		"workers",
		consumerWorkers,
		"max_attempts",
		maxProcessingAttempts,
		"retry_delay",
		retryDelay,
	)
	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	results := make(chan processingResult, 40)
	workerSlots := make(chan struct{}, consumerWorkers)

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-closed:
			if err == nil {
				return fmt.Errorf("rabbitmq connection closed")
			}
			return err
		case item := <-deliveries:
			workerSlots <- struct{}{}
			go func(work queuedDelivery) {
				handlerErr := handler(work.delivery.Body)
				<-workerSlots
				select {
				case results <- processingResult{item: work, err: handlerErr}:
				case <-ctx.Done():
				}
			}(item)
		case result := <-results:
			if result.err == nil {
				if err := result.item.delivery.Ack(false); err != nil {
					logger.Error("rabbitmq ack failed", "queue", result.item.queueName, "error", err)
				}
				continue
			}

			attempt := deliveryAttempt(result.item.delivery)
			if attempt >= maxProcessingAttempts {
				err := publishForRetry(
					ctx,
					channel,
					publishConfirmations,
					result.item.delivery,
					deadQueueName(result.item.queueName),
					attempt,
					result.err,
				)
				if err != nil {
					logger.Error("failed to publish event to dead-letter queue", "queue", result.item.queueName, "attempt", attempt, "error", err)
					_ = result.item.delivery.Nack(false, true)
					continue
				}
				logger.Error(
					"evolution event moved to dead-letter queue",
					"queue",
					result.item.queueName,
					"attempt",
					attempt,
					"error",
					result.err,
				)
				_ = result.item.delivery.Ack(false)
				continue
			}

			err := publishForRetry(
				ctx,
				channel,
				publishConfirmations,
				result.item.delivery,
				retryQueueName(result.item.queueName),
				attempt,
				result.err,
			)
			if err != nil {
				logger.Error("failed to schedule event retry", "queue", result.item.queueName, "attempt", attempt, "error", err)
				_ = result.item.delivery.Nack(false, true)
				continue
			}
			logger.Warn(
				"evolution event scheduled for retry",
				"queue",
				result.item.queueName,
				"attempt",
				attempt,
				"next_attempt",
				attempt+1,
				"error",
				result.err,
			)
			_ = result.item.delivery.Ack(false)
		}
	}
}

func declareReliabilityQueues(channel *amqp.Channel, sourceQueue string) error {
	if _, err := channel.QueueDeclare(
		retryQueueName(sourceQueue),
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-message-ttl":             int64(retryDelay / time.Millisecond),
			"x-dead-letter-exchange":    "",
			"x-dead-letter-routing-key": sourceQueue,
		},
	); err != nil {
		return fmt.Errorf("declare retry queue for %s: %w", sourceQueue, err)
	}

	if _, err := channel.QueueDeclare(
		deadQueueName(sourceQueue),
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare dead-letter queue for %s: %w", sourceQueue, err)
	}
	return nil
}

func publishForRetry(
	ctx context.Context,
	channel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	routingKey string,
	attempt int,
	processingErr error,
) error {
	headers := cloneHeaders(delivery.Headers)
	headers["x-chatwoot-attempt"] = int32(attempt)
	headers["x-chatwoot-last-error"] = truncateError(processingErr, 500)

	contentType := delivery.ContentType
	if contentType == "" {
		contentType = "application/json"
	}

	confirmCtx, cancel := context.WithTimeout(ctx, publishConfirmTimeout)
	defer cancel()
	err := channel.PublishWithContext(
		confirmCtx,
		"",
		routingKey,
		false,
		false,
		amqp.Publishing{
			Headers:         headers,
			ContentType:     contentType,
			ContentEncoding: delivery.ContentEncoding,
			DeliveryMode:    amqp.Persistent,
			Priority:        delivery.Priority,
			CorrelationId:   delivery.CorrelationId,
			ReplyTo:         delivery.ReplyTo,
			MessageId:       delivery.MessageId,
			Timestamp:       delivery.Timestamp,
			Type:            delivery.Type,
			UserId:          delivery.UserId,
			AppId:           delivery.AppId,
			Body:            delivery.Body,
		},
	)
	if err != nil {
		return err
	}

	select {
	case confirmation, ok := <-confirmations:
		if !ok {
			return fmt.Errorf("rabbitmq publisher confirmation channel closed")
		}
		if !confirmation.Ack {
			return fmt.Errorf("rabbitmq rejected published retry message")
		}
		return nil
	case <-confirmCtx.Done():
		return fmt.Errorf("waiting for rabbitmq publish confirmation: %w", confirmCtx.Err())
	}
}

func replayRecoverableDeadLetters(
	ctx context.Context,
	channel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
) (int, error) {
	recovered := 0
	for _, sourceQueue := range []string{"message", "sendmessage"} {
		queue, err := channel.QueueInspect(deadQueueName(sourceQueue))
		if err != nil {
			return recovered, fmt.Errorf("inspect dead-letter queue for %s: %w", sourceQueue, err)
		}

		unmatched := make([]amqp.Delivery, 0)
		for current := 0; current < queue.Messages; current++ {
			delivery, ok, err := channel.Get(queue.Name, false)
			if err != nil {
				requeueDeliveries(unmatched)
				return recovered, fmt.Errorf("read dead-letter queue for %s: %w", sourceQueue, err)
			}
			if !ok {
				break
			}
			if !isRecoverableDeadLetter(delivery.Headers) {
				unmatched = append(unmatched, delivery)
				continue
			}

			headers := cloneHeaders(delivery.Headers)
			delete(headers, "x-chatwoot-attempt")
			delete(headers, "x-chatwoot-last-error")
			if err := publishRecoveredDelivery(ctx, channel, confirmations, sourceQueue, delivery, headers); err != nil {
				_ = delivery.Nack(false, true)
				requeueDeliveries(unmatched)
				return recovered, fmt.Errorf("replay dead-letter event to %s: %w", sourceQueue, err)
			}
			if err := delivery.Ack(false); err != nil {
				requeueDeliveries(unmatched)
				return recovered, fmt.Errorf("ack replayed dead-letter event from %s: %w", sourceQueue, err)
			}
			recovered++
		}
		requeueDeliveries(unmatched)
	}
	return recovered, nil
}

func publishRecoveredDelivery(
	ctx context.Context,
	channel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	routingKey string,
	delivery amqp.Delivery,
	headers amqp.Table,
) error {
	contentType := delivery.ContentType
	if contentType == "" {
		contentType = "application/json"
	}

	confirmCtx, cancel := context.WithTimeout(ctx, publishConfirmTimeout)
	defer cancel()
	if err := channel.PublishWithContext(
		confirmCtx,
		"",
		routingKey,
		false,
		false,
		amqp.Publishing{
			Headers:         headers,
			ContentType:     contentType,
			ContentEncoding: delivery.ContentEncoding,
			DeliveryMode:    amqp.Persistent,
			Priority:        delivery.Priority,
			CorrelationId:   delivery.CorrelationId,
			ReplyTo:         delivery.ReplyTo,
			MessageId:       delivery.MessageId,
			Timestamp:       delivery.Timestamp,
			Type:            delivery.Type,
			UserId:          delivery.UserId,
			AppId:           delivery.AppId,
			Body:            delivery.Body,
		},
	); err != nil {
		return err
	}

	select {
	case confirmation, ok := <-confirmations:
		if !ok {
			return fmt.Errorf("rabbitmq publisher confirmation channel closed")
		}
		if !confirmation.Ack {
			return fmt.Errorf("rabbitmq rejected recovered dead-letter event")
		}
		return nil
	case <-confirmCtx.Done():
		return fmt.Errorf("waiting for rabbitmq recovery confirmation: %w", confirmCtx.Err())
	}
}

func requeueDeliveries(deliveries []amqp.Delivery) {
	for _, delivery := range deliveries {
		_ = delivery.Nack(false, true)
	}
}

func isRecoverableDeadLetter(headers amqp.Table) bool {
	if len(headers) == 0 {
		return false
	}
	lastError := fmt.Sprint(headers["x-chatwoot-last-error"])
	return strings.Contains(lastError, legacyAliasColumnError) ||
		strings.Contains(lastError, legacyCanonicalAliasColumnError)
}

func deliveryAttempt(delivery amqp.Delivery) int {
	previous := headerInt(delivery.Headers["x-chatwoot-attempt"])
	if previous < 0 {
		previous = 0
	}
	return previous + 1
}

func headerInt(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		if typed > uint64(^uint(0)>>1) {
			return 0
		}
		return int(typed)
	default:
		return 0
	}
}

func cloneHeaders(source amqp.Table) amqp.Table {
	cloned := make(amqp.Table, len(source)+2)
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func retryQueueName(sourceQueue string) string {
	return sourceQueue + ".chatwoot-retry"
}

func deadQueueName(sourceQueue string) string {
	return sourceQueue + ".chatwoot-dead"
}

func truncateError(err error, max int) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) <= max {
		return message
	}
	return message[:max]
}
