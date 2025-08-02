package audio

import (
	"context"
	"errors"
	"sync"

	"github.com/gen2brain/malgo"
)

type CaptureDevice struct {
	device    *malgo.Device
	frameSize int
	ctx       context.Context
	cancel    context.CancelFunc
	status    deviceStatus

	dataCallback func([]byte)
	stageLock    sync.Mutex
}

func (e *AudioEngine) NewCaptureDevice(
	ctx context.Context,
	format FormatType,
	channels,
	sampleRate,
	PeriodSizeInMilliseconds int,
) (*CaptureDevice, error) {
	ctx, cancel := context.WithCancel(ctx)

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatType(format)
	deviceConfig.Capture.Channels = uint32(channels)
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.PerformanceProfile = malgo.LowLatency
	deviceConfig.PeriodSizeInMilliseconds = uint32(PeriodSizeInMilliseconds)

	frameSize := channels * sampleRate * FormatSize(format) * PeriodSizeInMilliseconds / 1000
	capture := &CaptureDevice{
		frameSize: frameSize,
		ctx:       ctx,
		cancel:    cancel,
	}

	device, err := malgo.InitDevice(e.ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: func(_, input []byte, frameCount uint32) {
			capture.dataCallback(input)
		},
	})
	if err != nil {
		return nil, err
	}

	capture.device = device
	return capture, nil
}

func (c *CaptureDevice) CaptureStage(ctx context.Context, out chan<- []byte) (func() error, error) {
	c.stageLock.Lock()
	defer c.stageLock.Unlock()

	if c.status != statusInactive {
		return nil, errors.New("device already active")
	}

	buffer := NewFrameBuffer(c.frameSize, c.frameSize*4)

	c.dataCallback = func(input []byte) {
		buffer.Write(input)

		data := buffer.ReadFrame()
		for data != nil {
			select {
			case out <- data:
			default:
			}
			data = buffer.ReadFrame()
		}
	}

	if err := c.device.Start(); err != nil {
		return nil, err
	}

	c.status = statusActive

	return func() error {
		defer func() {
			c.device.Stop()
			c.stageLock.Lock()
			c.status = statusInactive
			c.stageLock.Unlock()
		}()

		for {
			select {
			case <-c.ctx.Done():
				return errors.New("device closed")
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}, nil
}

func (c *CaptureDevice) Pause() error {
	c.stageLock.Lock()
	defer c.stageLock.Unlock()

	if c.status == statusInactive {
		return errors.New("device inactive")
	}

	if c.status == statusPaused {
		return errors.New("already paused")
	}

	err := c.device.Stop()
	if err != nil {
		return err
	}

	c.status = statusPaused
	return nil
}

func (c *CaptureDevice) Resume() error {
	c.stageLock.Lock()
	defer c.stageLock.Unlock()

	if c.status == statusInactive {
		return errors.New("device inactive")
	}

	if c.status == statusActive {
		return errors.New("already active")
	}

	err := c.device.Start()
	if err != nil {
		return err
	}

	c.status = statusPaused
	return nil
}

func (c *CaptureDevice) Close() {
	c.cancel()
	c.device.Uninit()
}
