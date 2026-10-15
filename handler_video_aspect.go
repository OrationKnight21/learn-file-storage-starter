package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
)

func getVideoAspectRatio(filePath string) (string, error) {
	type parameter struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	type config struct {
		Streams []parameter `json:"streams"`
	}
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var b bytes.Buffer
	cmd.Stdout = &b
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	params := config{}
	err = json.Unmarshal(b.Bytes(), &params)
	if err != nil {
		return "", err
	}
	if len(params.Streams) < 1 {
		return "", errors.New("no parameters found")
	}
	ratio := float64(params.Streams[0].Width) / float64(params.Streams[0].Height)
	if math.Abs(ratio-9.0/16.0) < 0.01 {
		return "9:16", nil
	}
	if math.Abs(ratio-16.0/9.0) < 0.01 {
		return "16:9", nil
	}

	return "other", nil
}
func processVideoForFastStart(filePath string) (string, error) {
	outputPath := fmt.Sprintf("%s.processing", filePath)
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return outputPath, nil
}
