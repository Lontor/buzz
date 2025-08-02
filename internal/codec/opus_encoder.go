package codec

import (
	"context"
	"fmt"
	"unsafe"

	"buzz/internal/audio"

	"gopkg.in/hraban/opus.v2"
)

const (
	maxOpusPacketSize = 20048
)

type OpusEncoder struct {
	encoder    *opus.Encoder
	channels   int
	sampleRate int
	format     audio.FormatType
	encode     func(pcm, data []byte) (int, error)
}

func NewOpusEncoder(format audio.FormatType, channels, sampleRate int) (*OpusEncoder, error) {
	encoder, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus encoder: %v", err)
	}

	var encodeFunc func(pcm, data []byte) (int, error)
	switch format {
	case audio.FormatF32:
		encodeFunc = func(pcm, data []byte) (int, error) {
			return encoder.EncodeFloat32(
				unsafe.Slice((*float32)(unsafe.Pointer(&pcm[0])), len(pcm)/4),
				data,
			)
		}
	case audio.FormatS16:
		encodeFunc = func(pcm, data []byte) (int, error) {
			return encoder.Encode(
				unsafe.Slice((*int16)(unsafe.Pointer(&pcm[0])), len(pcm)/2),
				data,
			)
		}
	default:
		return nil, fmt.Errorf("format %v not supported", format)
	}

	return &OpusEncoder{
		encoder:    encoder,
		channels:   channels,
		sampleRate: sampleRate,
		format:     format,
		encode:     encodeFunc,
	}, nil
}

func (e *OpusEncoder) EncodeStage(
	ctx context.Context,
	in <-chan []byte,
	out chan<- []byte,
) func() error {
	return func() error {
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case pcm, ok := <-in:
				if !ok {
					return nil
				}

				data := make([]byte, maxOpusPacketSize)
				n, err := e.encode(pcm, data)
				if err != nil {
					return fmt.Errorf("encoding error: %w", err)
				}

				select {
				case out <- data[:n]:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}

func (e *OpusEncoder) SetBitrate(bitrate int) error {
	return e.encoder.SetBitrate(bitrate)
}

func (e *OpusEncoder) SetBitrateToAuto() error {
	return e.encoder.SetBitrateToAuto()
}

func (e *OpusEncoder) SetBitrateToMax() error {
	return e.encoder.SetBitrateToMax()
}

func (e *OpusEncoder) SetComplexity(complexity int) error {
	return e.encoder.SetComplexity(complexity)
}

func (e *OpusEncoder) SetDTX(enabled bool) error {
	return e.encoder.SetDTX(enabled)
}

func (e *OpusEncoder) SetInBandFEC(enabled bool) error {
	return e.encoder.SetInBandFEC(enabled)
}

func (e *OpusEncoder) SetMaxBandwidth(bandwidth opus.Bandwidth) error {
	return e.encoder.SetMaxBandwidth(bandwidth)
}

func (e *OpusEncoder) SetPacketLossPerc(percent int) error {
	return e.encoder.SetPacketLossPerc(percent)
}
