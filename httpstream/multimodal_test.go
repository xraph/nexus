package httpstream_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// TestNDJSON_AudioImageRoundTrip verifies that audio/image bytes survive a
// trip through the NDJSON encoder (base64) and a naive client-side decode.
func TestNDJSON_AudioImageRoundTrip(t *testing.T) {
	t.Parallel()

	audioBytes := make([]byte, 1024)
	if _, err := rand.Read(audioBytes); err != nil {
		t.Fatalf("rand: %v", err)
	}
	imageBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x00}

	chunks := []*provider.StreamChunk{
		{Provider: "rt", Model: "m", Kind: provider.EventAudio, Delta: provider.Delta{
			Audio: &provider.AudioChunk{
				Format:     "pcm16",
				SampleRate: 24000,
				Data:       audioBytes,
				Transcript: "partial transcript",
			},
		}},
		{Provider: "rt", Model: "m", Kind: provider.EventImage, Delta: provider.Delta{
			Image: &provider.ImageChunk{
				MimeType: "image/png",
				Data:     imageBytes,
			},
		}, FinishReason: "stop"},
	}

	stream := testutil.NewFakeStream(chunks, nil)
	enc := httpstream.NewNDJSONEncoder()
	var buf bytes.Buffer

	for {
		c, err := stream.Next(context.Background())
		if err != nil {
			break
		}
		if err := enc.EncodeEvent(&buf, httpstream.FromChunk(c, "")); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	if err := enc.End(&buf); err != nil {
		t.Fatalf("end: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (audio, image, done): %s", len(lines), buf.String())
	}

	// Audio line.
	var audioLine struct {
		Type  string `json:"type"`
		Delta struct {
			Audio *struct {
				Format     string `json:"format"`
				SampleRate int    `json:"sample_rate"`
				Data       string `json:"data"`
				Transcript string `json:"transcript"`
			} `json:"audio"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &audioLine); err != nil {
		t.Fatalf("audio decode: %v", err)
	}
	if audioLine.Type != "audio" {
		t.Fatalf("audio line type = %q", audioLine.Type)
	}
	if audioLine.Delta.Audio == nil {
		t.Fatal("audio missing in delta")
	}
	got, err := base64.StdEncoding.DecodeString(audioLine.Delta.Audio.Data)
	if err != nil {
		t.Fatalf("audio b64 decode: %v", err)
	}
	if !bytes.Equal(got, audioBytes) {
		t.Fatalf("audio bytes mismatch: %d vs %d", len(got), len(audioBytes))
	}
	if audioLine.Delta.Audio.Transcript != "partial transcript" {
		t.Fatalf("transcript = %q", audioLine.Delta.Audio.Transcript)
	}

	// Image line.
	var imageLine struct {
		Type  string `json:"type"`
		Delta struct {
			Image *struct {
				MimeType string `json:"mime_type"`
				Data     string `json:"data"`
			} `json:"image"`
		} `json:"delta"`
	}
	if imgErr := json.Unmarshal([]byte(lines[1]), &imageLine); imgErr != nil {
		t.Fatalf("image decode: %v", imgErr)
	}
	if imageLine.Type != "image" || imageLine.Delta.Image == nil {
		t.Fatalf("image line malformed: %+v", imageLine)
	}
	gotImg, err := base64.StdEncoding.DecodeString(imageLine.Delta.Image.Data)
	if err != nil {
		t.Fatalf("image b64 decode: %v", err)
	}
	if !bytes.Equal(gotImg, imageBytes) {
		t.Fatalf("image bytes mismatch")
	}
	if imageLine.Delta.Image.MimeType != "image/png" {
		t.Fatalf("mime = %q", imageLine.Delta.Image.MimeType)
	}
}

// TestAccumulator_MultiModal verifies that audio/image chunks survive
// Accumulate and land on resp.State for downstream consumers.
func TestAccumulator_MultiModal(t *testing.T) {
	t.Parallel()

	audio2 := []byte{5, 6, 7}
	audio1 := make([]byte, 0, 4+len(audio2))
	audio1 = append(audio1, 1, 2, 3, 4)
	image := []byte{0x89, 0x50, 0x4E}

	chunks := []*provider.StreamChunk{
		{Kind: provider.EventAudio, Delta: provider.Delta{Audio: &provider.AudioChunk{Format: "pcm16", SampleRate: 24000, Data: audio1, Transcript: "hello"}}},
		{Kind: provider.EventAudio, Delta: provider.Delta{Audio: &provider.AudioChunk{Data: audio2, Transcript: "hello world"}}},
		{Kind: provider.EventImage, Delta: provider.Delta{Image: &provider.ImageChunk{MimeType: "image/png", Data: image, Partial: false}}, FinishReason: "stop"},
	}
	stream := testutil.NewFakeStream(chunks, nil)
	resp, err := provider.Accumulate(context.Background(), stream)
	if err != nil {
		t.Fatalf("Accumulate: %v", err)
	}
	if resp.State == nil {
		t.Fatal("resp.State nil — multi-modal payloads not surfaced")
	}
	gotAudio, ok := resp.State["audio"].(provider.AudioChunk)
	if !ok {
		t.Fatalf("State[audio] missing/wrong type: %T", resp.State["audio"])
	}
	if !bytes.Equal(gotAudio.Data, append(audio1, audio2...)) {
		t.Fatalf("audio bytes not concatenated: got %v", gotAudio.Data)
	}
	if gotAudio.Transcript != "hello world" {
		t.Fatalf("transcript = %q", gotAudio.Transcript)
	}
	if gotAudio.Format != "pcm16" || gotAudio.SampleRate != 24000 {
		t.Fatalf("audio metadata lost: %+v", gotAudio)
	}
	gotImage, ok := resp.State["image"].(provider.ImageChunk)
	if !ok {
		t.Fatalf("State[image] missing")
	}
	if !bytes.Equal(gotImage.Data, image) || gotImage.MimeType != "image/png" {
		t.Fatalf("image lost: %+v", gotImage)
	}
}
