package player

import (
	"encoding/json"
	"errors"
	"os/exec"
)

type AudioMetadata struct {
	Codec    string `json:"codec_name"`
	Channels int    `json:"channels"`
}

type ffprobeOutput struct {
	Streams []AudioMetadata `json:"streams"`
}

// ProbeMedia uses ffprobe to identify the audio codec and channel count of a file.
func ProbeMedia(path string) (*AudioMetadata, error) {
	args := []string{
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name,channels",
		"-of", "json",
		path,
	}

	cmd := exec.Command("ffprobe", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result ffprobeOutput
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	if len(result.Streams) == 0 {
		return nil, errors.New("no audio streams found")
	}

	return &result.Streams[0], nil
}
