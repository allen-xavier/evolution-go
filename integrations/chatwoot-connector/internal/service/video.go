package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
)

// Evolution only accepts videos whose bytes are detected as video/mp4. Videos
// recorded on iPhones (.mov/QuickTime) and some Android/WhatsApp Web exports
// were refused, and the connector kept retrying the same file for one hour.
// They are now converted with ffmpeg: a fast remux when the video is already
// H.264, a re-encode otherwise. If conversion fails the file is sent as a
// document, so the contact still gets it.

const videoConversionTimeout = 4 * time.Minute

// isWhatsAppMP4 mirrors Evolution's check (mimetype detection on the bytes).
func isWhatsAppMP4(data []byte) bool {
	return mimetype.Detect(data).Is("video/mp4")
}

// prepareVideoForWhatsApp returns the video as MP4 (converted reports whether it changed).
func prepareVideoForWhatsApp(data []byte) (out []byte, converted bool, err error) {
	if isWhatsAppMP4(data) {
		return data, false, nil
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, false, errors.New("ffmpeg is not installed")
	}

	dir, err := os.MkdirTemp("", "chatwoot-video-*")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "input")
	output := filepath.Join(dir, "output.mp4")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		return nil, false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), videoConversionTimeout)
	defer cancel()

	common := []string{"-y", "-v", "error", "-i", input, "-map", "0:v:0", "-map", "0:a:0?"}
	tail := []string{"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", output}
	var attempts [][]string
	if videoCodec(ctx, input) == "h264" {
		// Same picture, new container: seconds, no quality loss.
		attempts = append(attempts, append(append(append([]string{}, common...), "-c:v", "copy"), tail...))
	}
	// Re-encode (HEVC from newer iPhones, VP8/VP9, 3GP...): H.264 that every phone plays.
	attempts = append(attempts, append(append(append([]string{}, common...),
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "26", "-pix_fmt", "yuv420p",
		"-vf", "scale='min(1280,iw)':-2"), tail...))

	var lastErr error
	for _, args := range attempts {
		if result, runErr := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); runErr != nil {
			lastErr = fmt.Errorf("ffmpeg: %v: %s", runErr, strings.TrimSpace(string(result)))
			continue
		}
		converted, readErr := os.ReadFile(output)
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if !isWhatsAppMP4(converted) {
			lastErr = errors.New("converted file is not detected as video/mp4")
			continue
		}
		return converted, true, nil
	}
	if lastErr == nil {
		lastErr = errors.New("video conversion failed")
	}
	return nil, false, lastErr
}

func videoCodec(ctx context.Context, path string) string {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name", "-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// withExtension swaps (or adds) the file extension: "IMG_0001.MOV" -> "IMG_0001.mp4".
func withExtension(fileName string, ext string) string {
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	if strings.TrimSpace(base) == "" {
		base = "video"
	}
	return base + ext
}

// isUnsupportedMediaError: Evolution refused the file itself (format or size);
// retrying the same file for an hour cannot help.
func isUnsupportedMediaError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"invalid file format",
		"is accepted",
		"invalid media type",
		"too large",
		"[413]",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
