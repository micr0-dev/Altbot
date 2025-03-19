// videoprocessor.go
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// ExtractVideoFrames extracts frames from a video at a specified FPS
func ExtractVideoFrames(videoData []byte, framesPerSecond float64, maxFrames int) ([]string, error) {
	// Create a temporary directory to store frames
	tempDir, err := os.MkdirTemp("", "videoframes")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up on exit

	// Create a temporary file for the video
	videoFile, err := os.CreateTemp(tempDir, "video-*.mp4")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp video file: %v", err)
	}
	videoPath := videoFile.Name()

	// Write video data to file
	if _, err := videoFile.Write(videoData); err != nil {
		videoFile.Close()
		return nil, fmt.Errorf("failed to write video data: %v", err)
	}
	videoFile.Close()

	// Create output pattern for frames
	framesPath := filepath.Join(tempDir, "frame-%04d.jpg")

	// Build FFmpeg command to extract frames
	cmd := exec.Command(
		"ffmpeg",
		"-i", videoPath, // Input file
		"-vf", fmt.Sprintf("fps=%f", framesPerSecond), // Extract at specified FPS
		"-q:v", "2", // Quality (2 is high quality)
		"-frames:v", strconv.Itoa(maxFrames), // Limit number of frames
		framesPath, // Output pattern
	)

	// Capture stderr for error reporting
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Run FFmpeg command
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg error: %v\nOutput: %s", err, stderr.String())
	}

	// Read all frame files
	frameFiles, err := filepath.Glob(filepath.Join(tempDir, "frame-*.jpg"))
	if err != nil {
		return nil, fmt.Errorf("failed to list frames: %v", err)
	}

	// Convert frames to base64
	var base64Frames []string
	for _, framePath := range frameFiles {
		// Read the frame file
		frameData, err := os.ReadFile(framePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read frame %s: %v", framePath, err)
		}

		// Encode as base64
		base64Frame := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(frameData)
		base64Frames = append(base64Frames, base64Frame)
	}

	return base64Frames, nil
}
