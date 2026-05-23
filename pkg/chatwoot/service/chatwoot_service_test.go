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
	number, formatJID = recipientForSend(lid)
	if number != lid {
		t.Fatalf("unexpected lid recipient: %s", number)
	}
	if formatJID == nil || *formatJID {
		t.Fatal("expected formatJid=false for lid sends")
	}
}
