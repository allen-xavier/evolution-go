package chatwoot_service

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestExtractEvolutionTextMessage(t *testing.T) {
	service := &chatwootService{
		httpClient: &http.Client{Timeout: time.Second},
	}

	raw := []byte(`{
		"event": "Message",
		"data": {
			"Info": {
				"Chat": "553193291010@s.whatsapp.net",
				"ID": "ABC123",
				"IsFromMe": false,
				"PushName": "Cliente"
			},
			"Message": {
				"conversation": "ola"
			}
		}
	}`)

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}

	msg, ok := service.extractEvolutionMessage(payload, raw)
	if !ok {
		t.Fatal("expected message to be extracted")
	}
	if msg.RemoteJID != "553193291010@s.whatsapp.net" {
		t.Fatalf("unexpected remote jid: %s", msg.RemoteJID)
	}
	if msg.Content != "ola" {
		t.Fatalf("unexpected content: %s", msg.Content)
	}
	if msg.MessageSourceID != "wa-in:ABC123" {
		t.Fatalf("unexpected source id: %s", msg.MessageSourceID)
	}
	if msg.FromMe {
		t.Fatal("expected incoming message")
	}
}

func TestExtractEvolutionMediaMessageWithBase64(t *testing.T) {
	service := &chatwootService{
		httpClient: &http.Client{Timeout: time.Second},
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("image-bytes"))
	raw := []byte(`{
		"event": "Message",
		"data": {
			"Info": {
				"Chat": "553193291010@s.whatsapp.net",
				"ID": "IMG123",
				"IsFromMe": false
			},
			"Message": {
				"base64": "` + encoded + `",
				"imageMessage": {
					"caption": "foto",
					"mimetype": "image/jpeg"
				}
			}
		}
	}`)

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}

	msg, ok := service.extractEvolutionMessage(payload, raw)
	if !ok {
		t.Fatal("expected message to be extracted")
	}
	if msg.Content != "foto" {
		t.Fatalf("unexpected content: %s", msg.Content)
	}
	if msg.MediaType != "image" {
		t.Fatalf("unexpected media type: %s", msg.MediaType)
	}
	if string(msg.Media) != "image-bytes" {
		t.Fatalf("unexpected media bytes: %q", string(msg.Media))
	}
}

func TestChatwootSourceIDAndRecipientForSend(t *testing.T) {
	instanceID := "b8592312-2083-476e-879c-509e68b7b337"
	remoteJID := "553193291010@s.whatsapp.net"
	sourceID := chatwootSourceID(instanceID, remoteJID)

	if got := remoteJIDFromSourceID(instanceID, sourceID); got != remoteJID {
		t.Fatalf("unexpected remote jid from source id: %s", got)
	}

	number, formatJID := recipientForSend(remoteJID)
	if number != "553193291010" {
		t.Fatalf("unexpected recipient number: %s", number)
	}
	if formatJID != nil {
		t.Fatal("expected nil formatJid for normal phone sends")
	}

	lid := "28462999949545@lid"
	lidSourceID := chatwootSourceID(instanceID, lid)
	expectedLIDSourceID := instanceID + ":lid:" + lid
	if lidSourceID != expectedLIDSourceID {
		t.Fatalf("unexpected lid source id: %s", lidSourceID)
	}
	if got := remoteJIDFromSourceID(instanceID, lidSourceID); got != lid {
		t.Fatalf("unexpected lid from source id: %s", got)
	}

	number, formatJID = recipientForSend(lid)
	if number != lid {
		t.Fatalf("unexpected lid recipient: %s", number)
	}
	if formatJID == nil || *formatJID {
		t.Fatal("expected formatJid=false for lid sends")
	}
}

func TestBuildChatwootContactRefDoesNotUseLIDAsPhone(t *testing.T) {
	instanceID := "eef4c22f-766f-4c77-a376-52219f57adfc"
	lid := "90465080737994@lid"

	ref := buildChatwootContactRef(instanceID, lid, "", true)
	if ref.Phone != "" {
		t.Fatalf("lid must not be used as phone number, got: %s", ref.Phone)
	}
	if ref.Identifier != instanceID+":lid:"+lid {
		t.Fatalf("unexpected lid identifier: %s", ref.Identifier)
	}
	if ref.SourceID != ref.Identifier {
		t.Fatalf("lid source id and identifier should match, got source=%s identifier=%s", ref.SourceID, ref.Identifier)
	}
	if ref.Name != lid {
		t.Fatalf("unexpected lid fallback name: %s", ref.Name)
	}

	phoneRef := buildChatwootContactRef(instanceID, "553193291010@s.whatsapp.net", "", true)
	if phoneRef.Phone == "" {
		t.Fatal("expected real WhatsApp JID to produce phone number")
	}
	if phoneRef.Identifier != "553193291010@s.whatsapp.net" {
		t.Fatalf("unexpected phone identifier: %s", phoneRef.Identifier)
	}
}

func TestResolveEvolutionRemoteJIDUsesSenderAltPhoneBeforeLID(t *testing.T) {
	service := &chatwootService{
		httpClient: &http.Client{Timeout: time.Second},
	}

	raw := []byte(`{
		"event": "Message",
		"instanceId": "eef4c22f-766f-4c77-a376-52219f57adfc",
		"data": {
			"Info": {
				"Chat": "90465080737994@lid",
				"Sender": "90465080737994@lid",
				"SenderAlt": "5516991635281@s.whatsapp.net",
				"ID": "LID123",
				"IsFromMe": false
			},
			"Message": {
				"conversation": "ola"
			}
		}
	}`)

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}

	msg, ok := service.extractEvolutionMessage(payload, raw)
	if !ok {
		t.Fatal("expected message to be extracted")
	}
	if msg.RemoteJID != "5516991635281@s.whatsapp.net" {
		t.Fatalf("unexpected remote jid: %s", msg.RemoteJID)
	}
}

func TestResolveEvolutionRemoteJIDUsesLIDResolver(t *testing.T) {
	service := &chatwootService{
		httpClient: &http.Client{Timeout: time.Second},
		lidResolver: func(instanceID string, lidJID string) (string, bool) {
			if instanceID != "eef4c22f-766f-4c77-a376-52219f57adfc" || lidJID != "90465080737994@lid" {
				return "", false
			}
			return "5516991635281@s.whatsapp.net", true
		},
	}

	raw := []byte(`{
		"event": "Message",
		"instanceId": "eef4c22f-766f-4c77-a376-52219f57adfc",
		"data": {
			"Info": {
				"Chat": "90465080737994@lid",
				"Sender": "90465080737994@lid",
				"ID": "LID123",
				"IsFromMe": false
			},
			"Message": {
				"conversation": "ola"
			}
		}
	}`)

	var payload evolutionWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}

	msg, ok := service.extractEvolutionMessage(payload, raw)
	if !ok {
		t.Fatal("expected message to be extracted")
	}
	if msg.RemoteJID != "5516991635281@s.whatsapp.net" {
		t.Fatalf("unexpected remote jid: %s", msg.RemoteJID)
	}
}

func TestToE164DoesNotRemoveBrazilNinthDigitForDDD16(t *testing.T) {
	got := toE164("5516991635281@s.whatsapp.net", true)
	if got != "+5516991635281" {
		t.Fatalf("unexpected ddd16 phone: %s", got)
	}
}
