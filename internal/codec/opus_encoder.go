package codec

import (
	"buzz/internal/audio"
	"fmt"
	"unsafe"

	"gopkg.in/hraban/opus.v2"
)

type OpusEncoder struct {
	encoder    *opus.Encoder
	sampleRate int
	channels   int
	encode     func(pcm, data []byte) (int, error)
}

func NewOpusEncoder(format audio.FormatType, channels, sampleRate int) (*OpusEncoder, error) {
	encoder, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus encoder: %v", err)
	}

	var encode func(pcm, data []byte) (int, error)
	switch format {
	case audio.FormatF32:
		encode = func(pcm, data []byte) (int, error) {
			return encoder.EncodeFloat32(unsafe.Slice((*float32)(unsafe.Pointer(&pcm[0])), len(pcm)/4), data)
		}
	case audio.FormatS16:
		encode = func(pcm, data []byte) (int, error) {
			return encoder.Encode(unsafe.Slice((*int16)(unsafe.Pointer(&pcm[0])), len(pcm)/2), data)
		}
	default:
		return nil, fmt.Errorf("failed to create Opus encoder: format %v not supported", format)
	}

	return &OpusEncoder{
		encoder:    encoder,
		sampleRate: sampleRate,
		channels:   channels,
		encode:     encode,
	}, nil
}

func (e *OpusEncoder) Encode(pcm, data []byte) (int, error) {
	return e.encode(pcm, data)
}
