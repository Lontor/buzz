package codec

import (
	"context"
	"fmt"
	"unsafe"

	"buzz/internal/audio"

	"gopkg.in/hraban/opus.v2"
)

type DecodeTask struct {
	Data         []byte
	RestoreFrame bool
}

type OpusDecoder struct {
	decoder                  *opus.Decoder
	channels                 int
	sampleRate               int
	PeriodSizeInMilliseconds int
	format                   audio.FormatType
	decode                   func(data, pcm []byte) (int, error)
	restore                  func(data, pcm []byte) error
}

func NewOpusDecoder(format audio.FormatType, channels, sampleRate, PeriodSizeInMilliseconds int) (*OpusDecoder, error) {
	decoder, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus decoder: %v", err)
	}

	var decodeFn func(data, pcm []byte) (int, error)
	var restoreFn func(data, pcm []byte) error

	switch format {
	case audio.FormatF32:
		decodeFn = func(data, pcm []byte) (int, error) {
			n, err := decoder.DecodeFloat32(
				data,
				unsafe.Slice((*float32)(unsafe.Pointer(&pcm[0])), len(pcm)/4),
			)
			return n * 4, err
		}
		restoreFn = func(data, pcm []byte) error {
			return decoder.DecodeFECFloat32(
				data,
				unsafe.Slice((*float32)(unsafe.Pointer(&pcm[0])), len(pcm)/4),
			)
		}
	case audio.FormatS16:
		decodeFn = func(data, pcm []byte) (int, error) {
			n, err := decoder.Decode(
				data,
				unsafe.Slice((*int16)(unsafe.Pointer(&pcm[0])), len(pcm)/2),
			)
			return n * 2, err
		}
		restoreFn = func(data, pcm []byte) error {
			return decoder.DecodeFEC(
				data,
				unsafe.Slice((*int16)(unsafe.Pointer(&pcm[0])), len(pcm)/2),
			)
		}
	default:
		return nil, fmt.Errorf("format %v not supported", format)
	}

	return &OpusDecoder{
		decoder:                  decoder,
		channels:                 channels,
		sampleRate:               sampleRate,
		format:                   format,
		PeriodSizeInMilliseconds: PeriodSizeInMilliseconds,
		decode:                   decodeFn,
		restore:                  restoreFn,
	}, nil
}

func (d *OpusDecoder) DecodeStage(
	ctx context.Context,
	in <-chan DecodeTask,
	out chan<- []byte,
) func() error {
	frameSizeBytes := d.channels * audio.FormatSize(d.format) * d.sampleRate * d.PeriodSizeInMilliseconds / 1000

	return func() error {
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case task, ok := <-in:
				if !ok {
					return nil
				}

				pcm := make([]byte, frameSizeBytes)

				var err error

				if task.RestoreFrame {
					err = d.restore(task.Data, pcm)
					if err != nil {
						return fmt.Errorf("FEC decode error: %w", err)
					}
				} else {
					_, err = d.decode(task.Data, pcm)
					if err != nil {
						return fmt.Errorf("decode error: %w", err)
					}
				}

				select {
				case out <- pcm:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}
