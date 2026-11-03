package repository

import "testing"

func TestLIDUser(t *testing.T) {
	for input, want := range map[string]string{
		"261370285142254@lid":     "261370285142254",
		" 261370285142254:12@LID": "261370285142254",
		"261370285142254.0:3@lid": "261370285142254",
	} {
		got, ok := lidUser(input)
		if !ok || got != want {
			t.Fatalf("lidUser(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"", "5511953774844@s.whatsapp.net", "abc@lid", "@lid", "261370285142254"} {
		if got, ok := lidUser(input); ok {
			t.Fatalf("lidUser(%q) = %q, want not ok", input, got)
		}
	}
}

func TestPhoneJIDFromPN(t *testing.T) {
	if got, ok := phoneJIDFromPN("5511953774844"); !ok || got != "5511953774844@s.whatsapp.net" {
		t.Fatalf("phoneJIDFromPN = %q, %v", got, ok)
	}
	for _, input := range []string{"", "  ", "55119x"} {
		if got, ok := phoneJIDFromPN(input); ok {
			t.Fatalf("phoneJIDFromPN(%q) = %q, want not ok", input, got)
		}
	}
}

func TestNilDBResolverNeverResolves(t *testing.T) {
	if got, ok := NewWhatsmeowLIDResolver(nil)("inst", "261370285142254@lid"); ok {
		t.Fatalf("nil db resolved %q", got)
	}
}
