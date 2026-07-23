package broker

import (
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestDeliveryAttemptUsesPersistentHeader(t *testing.T) {
	tests := []struct {
		name    string
		headers amqp.Table
		want    int
	}{
		{name: "first delivery", headers: nil, want: 1},
		{name: "second delivery", headers: amqp.Table{"x-chatwoot-attempt": int32(1)}, want: 2},
		{name: "int64 header", headers: amqp.Table{"x-chatwoot-attempt": int64(7)}, want: 8},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := deliveryAttempt(amqp.Delivery{Headers: test.headers})
			if got != test.want {
				t.Fatalf("deliveryAttempt() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestReliabilityQueueNamesAndBoundedError(t *testing.T) {
	if got := retryQueueName("message"); got != "message.chatwoot-retry" {
		t.Fatalf("unexpected retry queue: %s", got)
	}
	if got := deadQueueName("sendmessage"); got != "sendmessage.chatwoot-dead" {
		t.Fatalf("unexpected dead queue: %s", got)
	}
	if got := truncateError(errors.New("123456"), 4); got != "1234" {
		t.Fatalf("unexpected truncated error: %s", got)
	}
}

func TestRecoverableDeadLetterDetection(t *testing.T) {
	if !isRecoverableDeadLetter(amqp.Table{
		"x-chatwoot-last-error": `save alias failed: ERROR: column "alias_jid" does not exist (SQLSTATE 42703)`,
	}) {
		t.Fatal("expected legacy alias column error to be recoverable")
	}
	if !isRecoverableDeadLetter(amqp.Table{
		"x-chatwoot-last-error": "save alias failed: ERROR: column excluded.canonical_jid does not exist (SQLSTATE 42703)",
	}) {
		t.Fatal("expected legacy canonical alias column error to be recoverable")
	}
	if isRecoverableDeadLetter(amqp.Table{
		"x-chatwoot-last-error": "chatwoot returned 500",
	}) {
		t.Fatal("unrelated dead-letter error must not be replayed automatically")
	}
}
