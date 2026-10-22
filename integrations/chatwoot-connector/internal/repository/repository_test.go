package repository

import (
	"sync"
	"testing"

	chatwoot_model "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
	"gorm.io/gorm/schema"
)

func TestMessageReceiptColumnsMatchQueries(t *testing.T) {
	parsed, err := schema.Parse(&chatwoot_model.ChatwootMessageReceipt{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse message receipt schema: %v", err)
	}
	// Columns written by hand in SaveMessageReceipt / GetMessageReceipt / UpdateMessageReceiptStatus.
	for field, column := range map[string]string{
		"InstanceID":        "instance_id",
		"WhatsAppMessageID": "whats_app_message_id",
		"ConversationID":    "conversation_id",
		"ChatwootMessageID": "chatwoot_message_id",
		"Status":            "status",
		"CreatedAt":         "created_at",
	} {
		got := parsed.LookUpField(field)
		if got == nil || got.DBName != column {
			t.Fatalf("%s column = %v, want %q", field, got, column)
		}
	}
}

func TestIdentityAliasColumnsMatchGORMModel(t *testing.T) {
	parsed, err := schema.Parse(
		&chatwoot_model.ChatwootIdentityAlias{},
		&sync.Map{},
		schema.NamingStrategy{},
	)
	if err != nil {
		t.Fatalf("parse identity alias schema: %v", err)
	}

	field := parsed.LookUpField("AliasJID")
	if field == nil {
		t.Fatal("AliasJID field was not found in the GORM schema")
	}
	if field.DBName != identityAliasJIDColumn {
		t.Fatalf("AliasJID database column = %q, want %q", field.DBName, identityAliasJIDColumn)
	}

	canonicalField := parsed.LookUpField("CanonicalJID")
	if canonicalField == nil {
		t.Fatal("CanonicalJID field was not found in the GORM schema")
	}
	if canonicalField.DBName != identityCanonicalJIDColumn {
		t.Fatalf("CanonicalJID database column = %q, want %q", canonicalField.DBName, identityCanonicalJIDColumn)
	}
}
