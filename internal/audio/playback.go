package audio

import (
	"context"
	"errors"
	"sync"

	"github.com/gen2brain/malgo"
)

type PlaybackDevice struct {
	device    *malgo.Device
	frameSize int
	ctx       context.Context
	cancel    context.CancelFunc
	status    deviceStatus
	stageLock sync.Mutex

	fillCallback func([]byte)
}

func (e *AudioEngine) NewPlaybackDevice(
	ctx context.Context,
	format FormatType,
	channels,
	sampleRate,
	PeriodSizeInMilliseconds int,
) (*PlaybackDevice, error) {
	ctx, cancel := context.WithCancel(ctx)

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatType(format)
	deviceConfig.Playback.Channels = uint32(channels)
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.PerformanceProfile = malgo.LowLatency
	deviceConfig.PeriodSizeInMilliseconds = uint32(PeriodSizeInMilliseconds)

	frameSize := channels * sampleRate * FormatSize(format) * PeriodSizeInMilliseconds / 1000

	playback := &PlaybackDevice{
		frameSize: frameSize,
		ctx:       ctx,
		cancel:    cancel,
	}

	device, err := malgo.InitDevice(e.ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: func(output, _ []byte, frameCount uint32) {
			playback.fillCallback(output)
		},
	})
	if err != nil {
		return nil, err
	}

	playback.device = device
	return playback, nil
}

func (p *PlaybackDevice) PlaybackStage(ctx context.Context, in <-chan []byte) (func() error, error) {
	p.stageLock.Lock()
	defer p.stageLock.Unlock()

	if p.status != statusInactive {
		return nil, errors.New("device already active")
	}

	buffer := NewFrameBuffer(p.frameSize, p.frameSize*4)

	p.fillCallback = func(output []byte) {
	loop:
		for {
			select {
			case data := <-in:
				buffer.Write(data)
			default:
				break loop
			}
		}

		data := buffer.ReadFrame()
		if data != nil {
			copy(output, data)
		}
	}

	if err := p.device.Start(); err != nil {
		return nil, err
	}

	p.status = statusActive

	return func() error {
		defer func() {
			p.device.Stop()
			p.stageLock.Lock()
			p.status = statusInactive
			p.stageLock.Unlock()
		}()

		select {
		case <-p.ctx.Done():
			return errors.New("device closed")
		case <-ctx.Done():
			return ctx.Err()
		}
	}, nil
}

func (p *PlaybackDevice) Pause() error {
	p.stageLock.Lock()
	defer p.stageLock.Unlock()

	if p.status == statusInactive {
		return errors.New("device inactive")
	}

	if p.status == statusPaused {
		return errors.New("already paused")
	}

	err := p.device.Stop()
	if err != nil {
		return err
	}

	p.status = statusPaused
	return nil
}

func (p *PlaybackDevice) Resume() error {
	p.stageLock.Lock()
	defer p.stageLock.Unlock()

	if p.status == statusInactive {
		return errors.New("device inactive")
	}

	if p.status == statusActive {
		return errors.New("already active")
	}

	err := p.device.Start()
	if err != nil {
		return err
	}

	p.status = statusActive
	return nil
}

func (p *PlaybackDevice) Close() {
	p.cancel()
	p.device.Uninit()
}
