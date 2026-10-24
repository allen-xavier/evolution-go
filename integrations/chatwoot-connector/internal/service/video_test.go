package service

import (
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	"github.com/patrickmn/go-cache"
)

// Minimal ISO-BMFF headers: "isom" brand = MP4, "qt  " brand = QuickTime (.MOV from iPhone).
var (
	mp4Header = []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2\x00\x00\x00\x08free")
	movHeader = []byte("\x00\x00\x00\x14ftypqt  \x00\x00\x02\x00qt  \x00\x00\x00\x08wide")
)

func dataURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func videoTestService(fake *outboundFakeEvolution, preparer func([]byte) ([]byte, bool, error)) *chatwootService {
	return &chatwootService{
		evolutionClient: fake,
		loggerWrapper:   testLoggerManager(),
		skipCache:       cache.New(time.Minute, time.Minute),
		videoPreparer:   preparer,
	}
}

func TestIsWhatsAppMP4MatchesEvolutionCheck(t *testing.T) {
	if !isWhatsAppMP4(mp4Header) {
		t.Fatal("MP4 must be accepted as is")
	}
	if isWhatsAppMP4(movHeader) {
		t.Fatal(".MOV (QuickTime) must be converted: Evolution only accepts video/mp4")
	}
}

func TestSendMediaConvertsNonMP4VideoBeforeSending(t *testing.T) {
	fake := &outboundFakeEvolution{}
	service := videoTestService(fake, func(data []byte) ([]byte, bool, error) {
		return mp4Header, true, nil
	})
	instance := &evolution.Instance{Id: "inst", Token: "token"}

	err := service.sendMediaFromChatwoot(instance, "553199990000@s.whatsapp.net",
		dataURL("video/quicktime", movHeader), "video", "IMG_0001.MOV", "olha", "501")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.mediaCalls) != 1 {
		t.Fatalf("expected one media send, got %d", len(fake.mediaCalls))
	}
	sent := fake.mediaCalls[0]
	if sent.Type != "video" || sent.Filename != "IMG_0001.mp4" || string(sent.Data) != string(mp4Header) {
		t.Fatalf("unexpected media request: type=%s file=%s", sent.Type, sent.Filename)
	}
}

func TestSendMediaFallsBackToDocumentWhenConversionFails(t *testing.T) {
	fake := &outboundFakeEvolution{}
	service := videoTestService(fake, func(data []byte) ([]byte, bool, error) {
		return nil, false, errors.New("ffmpeg failed")
	})
	err := service.sendMediaFromChatwoot(&evolution.Instance{Id: "inst", Token: "token"}, "553199990000@s.whatsapp.net",
		dataURL("video/quicktime", movHeader), "video", "IMG_0002.MOV", "", "502")
	if err != nil {
		t.Fatal(err)
	}
	sent := fake.mediaCalls[0]
	if sent.Type != "document" || sent.Filename != "IMG_0002.MOV" || string(sent.Data) != string(movHeader) {
		t.Fatalf("expected the original file as document, got type=%s file=%s", sent.Type, sent.Filename)
	}
}

func TestSendMediaKeepsMP4Untouched(t *testing.T) {
	fake := &outboundFakeEvolution{}
	service := videoTestService(fake, nil) // real preparer: MP4 needs no ffmpeg
	err := service.sendMediaFromChatwoot(&evolution.Instance{Id: "inst", Token: "token"}, "553199990000@s.whatsapp.net",
		dataURL("video/mp4", mp4Header), "video", "clip.mp4", "", "503")
	if err != nil {
		t.Fatal(err)
	}
	if sent := fake.mediaCalls[0]; sent.Type != "video" || sent.Filename != "clip.mp4" {
		t.Fatalf("MP4 must go unchanged: type=%s file=%s", sent.Type, sent.Filename)
	}
}

func TestRefusedMediaIsPermanentAndExplained(t *testing.T) {
	err := errors.New(`evolution request failed [400]: {"error":"Invalid file format: 'video/quicktime'. Only 'video/mp4' is accepted"}`)
	if !isPermanentOutboundError(err) || !isUnsupportedMediaError(err) {
		t.Fatal("a refused file must fail at once instead of retrying for one hour")
	}
	if isUnsupportedMediaError(errors.New("context deadline exceeded")) {
		t.Fatal("a timeout is not a refused file")
	}
	if withExtension("IMG.MOV", ".mp4") != "IMG.mp4" || withExtension("", ".mp4") != "video.mp4" {
		t.Fatal("unexpected file name")
	}
}

// Runs only where ffmpeg exists (the connector image has it): a real .MOV becomes MP4.
func TestPrepareVideoConvertsRealMOV(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	mov := filepath.Join(dir, "sample.mov")
	for _, codec := range []string{"libx264", "mpeg4"} { // H.264 → remux; MPEG-4 Part 2 → re-encode
		out, err := exec.Command("ffmpeg", "-y", "-v", "error",
			"-f", "lavfi", "-i", "testsrc=duration=1:size=320x240:rate=15",
			"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
			"-c:v", codec, "-c:a", "aac", "-shortest", "-f", "mov", mov).CombinedOutput()
		if err != nil {
			t.Fatalf("create sample (%s): %v %s", codec, err, out)
		}
		data, _ := os.ReadFile(mov)
		if isWhatsAppMP4(data) {
			t.Fatalf("sample (%s) should be QuickTime, not MP4", codec)
		}
		converted, changed, err := prepareVideoForWhatsApp(data)
		if err != nil || !changed || !isWhatsAppMP4(converted) {
			t.Fatalf("conversion (%s) failed: changed=%v err=%v", codec, changed, err)
		}
	}
}
