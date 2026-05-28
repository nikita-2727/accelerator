package tools

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GetAudioDurationSeconds возвращает округлённую до секунды длительность
// аудиофайла через ffprobe. Работает на любом формате, который понимает
// ffmpeg (MP3, WAV, OGG/Opus, FLAC, AAC/M4A и т. д.).
func GetAudioDurationSeconds(ctx context.Context, path string, limitLoadDurationSeconds time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, limitLoadDurationSeconds)
	defer cancel()

	cmd := exec.CommandContext(ctx,
		"ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)

	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" || raw == "N/A" {
		return 0, fmt.Errorf("ffprobe: длительность не определена")
	}

	dur, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("ffprobe: разбор результата %q: %w", raw, err)
	}
	if dur <= 0 {
		return 0, fmt.Errorf("ffprobe: некорректная длительность %v", dur)
	}
	return int(math.Round(dur)), nil
}
