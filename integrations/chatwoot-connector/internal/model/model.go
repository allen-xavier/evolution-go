package model

import "time"

type ChatwootConfig struct {
	InstanceID string `json:"instanceId" gorm:"type:uuid;primaryKey"`

	Enabled bool `json:"enabled" gorm:"default:false"`

	URL             string `json:"url"`
	AccountID       string `json:"accountId"`
	Token           string `json:"token"`
	InboxID         int    `json:"inboxId"`
	InboxIdentifier string `json:"inboxIdentifier"`

	SignMsg             bool `json:"signMsg" gorm:"default:true"`
	ReopenConversation  bool `json:"reopenConversation" gorm:"default:true"`
	ConversationPending bool `json:"conversationPending" gorm:"default:false"`
	MergeBrazilContacts bool `json:"mergeBrazilContacts" gorm:"default:true"`
	ImportContacts      bool `json:"importContacts" gorm:"default:true"`
	ImportMessages      bool `json:"importMessages" gorm:"default:true"`

	DaysLimitImportMessages int    `json:"daysLimitImportMessages" gorm:"default:3"`
	NameInbox               string `json:"nameInbox"`
	SignDelimiter           string `json:"signDelimiter" gorm:"default:'\\n'"`
	Organization            string `json:"organization"`
	Logo                    string `json:"logo"`
	IgnoreJids              string `json:"ignoreJids" gorm:"type:text"`

	WebhookSecret string `json:"webhookSecret"`

	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

type ChatwootBinding struct {
	ID uint `json:"id" gorm:"primaryKey"`

	InstanceID string `json:"instanceId" gorm:"type:uuid;index:idx_chatwoot_binding_instance_jid,unique;index:idx_chatwoot_binding_instance_conversation,unique"`
	RemoteJID  string `json:"remoteJid" gorm:"size:191;index:idx_chatwoot_binding_instance_jid,unique"`

	ContactID      int    `json:"contactId"`
	ConversationID int    `json:"conversationId" gorm:"index:idx_chatwoot_binding_instance_conversation,unique"`
	SourceID       string `json:"sourceId"`

	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

// ChatwootIdentityAlias keeps the WhatsApp LID-to-phone relationship learned
// from later events. The first event for a contact may contain only a LID; once
// WhatsApp includes SenderAlt/RecipientAlt, subsequent LID-only events can be
// routed to the same Chatwoot conversation.
type ChatwootIdentityAlias struct {
	InstanceID   string `json:"instanceId" gorm:"type:uuid;primaryKey"`
	AliasJID     string `json:"aliasJid" gorm:"column:alias_j_id;size:191;primaryKey"`
	CanonicalJID string `json:"canonicalJid" gorm:"column:canonical_j_id;size:191;index:idx_chatwoot_identity_canonical"`

	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

// ChatwootOutboundJob is a durable copy of a Chatwoot webhook that Evolution
// could not send to WhatsApp. Jobs are retried until delivery succeeds.
type ChatwootOutboundJob struct {
	ID uint `json:"id" gorm:"primaryKey"`

	InstanceID        string `json:"instanceId" gorm:"type:uuid;uniqueIndex:idx_chatwoot_outbound_message"`
	ChatwootMessageID string `json:"chatwootMessageId" gorm:"size:191;uniqueIndex:idx_chatwoot_outbound_message"`
	Payload           []byte `json:"-" gorm:"type:bytea"`

	Attempts      int       `json:"attempts" gorm:"default:0"`
	NextAttemptAt time.Time `json:"nextAttemptAt" gorm:"index:idx_chatwoot_outbound_due"`
	LastError     string    `json:"lastError" gorm:"type:text"`

	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

type SetChatwootPayload struct {
	Enabled bool `json:"enabled"`

	AccountID string `json:"accountId"`
	Token     string `json:"token"`
	URL       string `json:"url"`
	InboxID   int    `json:"inboxId"`

	InboxIdentifier string `json:"inboxIdentifier"`

	SignMsg             bool `json:"signMsg"`
	ReopenConversation  bool `json:"reopenConversation"`
	ConversationPending bool `json:"conversationPending"`
	MergeBrazilContacts bool `json:"mergeBrazilContacts"`
	ImportContacts      bool `json:"importContacts"`
	ImportMessages      bool `json:"importMessages"`

	DaysLimitImportMessages int      `json:"daysLimitImportMessages"`
	NameInbox               string   `json:"nameInbox"`
	SignDelimiter           string   `json:"signDelimiter"`
	Organization            string   `json:"organization"`
	Logo                    string   `json:"logo"`
	IgnoreJids              []string `json:"ignoreJids"`
	WebhookSecret           string   `json:"webhookSecret"`
}

type ChatwootConfigView struct {
	Enabled bool `json:"enabled"`

	AccountID string `json:"accountId"`
	Token     string `json:"token"`
	URL       string `json:"url"`
	InboxID   int    `json:"inboxId"`

	InboxIdentifier string `json:"inboxIdentifier"`

	SignMsg             bool `json:"signMsg"`
	ReopenConversation  bool `json:"reopenConversation"`
	ConversationPending bool `json:"conversationPending"`
	MergeBrazilContacts bool `json:"mergeBrazilContacts"`
	ImportContacts      bool `json:"importContacts"`
	ImportMessages      bool `json:"importMessages"`

	DaysLimitImportMessages int      `json:"daysLimitImportMessages"`
	NameInbox               string   `json:"nameInbox"`
	SignDelimiter           string   `json:"signDelimiter"`
	Organization            string   `json:"organization"`
	Logo                    string   `json:"logo"`
	IgnoreJids              []string `json:"ignoreJids"`
	WebhookSecret           string   `json:"webhookSecret"`
}
