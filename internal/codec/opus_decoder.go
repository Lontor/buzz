package codec

import (
	"buzz/internal/audio"
	"fmt"
	"unsafe"

	"gopkg.in/hraban/opus.v2"
)

type OpusDecoder struct {
	decoder    *opus.Decoder
	sampleRate int
	channels   int
	decode     func(data, pcm []byte) (int, error)
}

func NewOpusDecoder(format audio.FormatType, channels, sampleRate int) (*OpusDecoder, error) {
	decoder, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus decoder: %v", err)
	}

	var decode func(data, pcm []byte) (int, error)
	switch format {
	case audio.FormatF32:
		decode = func(data, pcm []byte) (int, error) {
			n, err := decoder.DecodeFloat32(data, unsafe.Slice((*float32)(unsafe.Pointer(&pcm[0])), len(pcm)/4))
			return n * 4, err
		}
	case audio.FormatS16:
		decode = func(data, pcm []byte) (int, error) {
			n, err := decoder.Decode(data, unsafe.Slice((*int16)(unsafe.Pointer(&pcm[0])), len(pcm)/2))
			return n * 2, err
		}
	default:
		return nil, fmt.Errorf("failed to create Opus decoder: format %v not supported", format)
	}

	return &OpusDecoder{
		decoder:    decoder,
		sampleRate: sampleRate,
		channels:   channels,
		decode:     decode,
	}, nil
}

func (d *OpusDecoder) Decode(data, pcm []byte) (int, error) {
	return d.decode(data, pcm)
}
