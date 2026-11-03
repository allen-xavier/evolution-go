package repository

import (
	"strings"

	"gorm.io/gorm"
)

// NewWhatsmeowLIDResolver resolves a LID (e.g. "261370285142254@lid") to the
// phone JID using the whatsmeow_lid_map table that Evolution keeps in the same
// database. whatsmeow stores the mapping as soon as a message carries the
// phone (SenderAlt), even when that message arrives undecryptable; the copy the
// phone resends later comes with the LID only. Without this lookup the contact
// was created in Chatwoot without a phone number and the robot stopped.
//
// The table is shared by every instance, so instanceID is not used. Any error
// (table missing, connection) means "not resolved".
func NewWhatsmeowLIDResolver(db *gorm.DB) func(instanceID string, lidJID string) (string, bool) {
	return func(_ string, lidJID string) (string, bool) {
		user, ok := lidUser(lidJID)
		if !ok || db == nil {
			return "", false
		}
		var pn string
		if err := db.Raw("SELECT pn FROM whatsmeow_lid_map WHERE lid = ? LIMIT 1", user).Scan(&pn).Error; err != nil {
			return "", false
		}
		return phoneJIDFromPN(pn)
	}
}

// lidUser returns the user part of a LID JID, without device and server.
func lidUser(jid string) (string, bool) {
	jid = strings.ToLower(strings.TrimSpace(jid))
	user, server, found := strings.Cut(jid, "@")
	if !found || server != "lid" {
		return "", false
	}
	user, _, _ = strings.Cut(user, ":")
	user, _, _ = strings.Cut(user, ".")
	if user == "" || !onlyDigits(user) {
		return "", false
	}
	return user, true
}

func phoneJIDFromPN(pn string) (string, bool) {
	pn = strings.TrimSpace(pn)
	if pn == "" || !onlyDigits(pn) {
		return "", false
	}
	return pn + "@s.whatsapp.net", true
}

func onlyDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}
