package repository

import (
	"sync"
	"testing"

	chatwoot_model "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
	"gorm.io/gorm/schema"
)

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
