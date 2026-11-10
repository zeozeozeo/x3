package videothumb

import (
	"context"

	"github.com/zeozeozeo/x3/ffmpegbox"
)

// runFFmpeg decodes a single frame from the video with the configured
// backend: the gVisor sandbox when enabled, otherwise the host binary.
func runFFmpeg(ctx context.Context, cfg ffmpegbox.Config, data []byte, seek string) ([]byte, error) {
	return ffmpegbox.Run(ctx, data, ffmpegbox.ToolFFmpeg, func(input string) []string {
		return ffmpegArgs(input, seek)
	}, MaxThumbnailBytes+1)
}

// ffmpegArgs seeks into the input, grabs one frame, bounds it to 1280px wide
// and writes a single JPEG to stdout.
func ffmpegArgs(input string, seek string) []string {
	return []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-ss", seek,
		"-i", input,
		"-frames:v", "1", "-q:v", "3",
		"-vf", "scale=w=1280:h=-2:force_original_aspect_ratio=decrease",
		"-f", "mjpeg", "-",
	}
}

// runFFmpegImpl is indirected so tests can fake the backend.
var runFFmpegImpl = runFFmpeg
