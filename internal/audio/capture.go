package audio

import (
	"errors"

	"github.com/gen2brain/malgo"
)

type CaptureDevice struct {
	device  *malgo.Device
	handler HandlerFunc
}

func NewCaptureDevice(ctx malgo.AllocatedContext, format FormatType, channels, sampleRate int) (*CaptureDevice, error) {
	capture := &CaptureDevice{}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatType(format)
	deviceConfig.Capture.Channels = uint32(channels)
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.PerformanceProfile = malgo.LowLatency

	callback := func(_, input []byte, frameCount uint32) {
		capture.handler(input, int(frameCount))
	}

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: callback,
	})
	if err != nil {
		return nil, err
	}
	capture.device = device
	return capture, nil
}

func (c *CaptureDevice) SetHandler(handler HandlerFunc) error {
	if handler == nil {
		return errors.New("audio handler is not set")
	}
	c.handler = handler
	return nil
}

func (c *CaptureDevice) Start() error {
	if c.handler == nil {
		return errors.New("audio handler is not set")
	}
	return c.device.Start()
}

func (c *CaptureDevice) Stop() error {
	return c.device.Stop()
}

func (c *CaptureDevice) SampleRate() int {
	return int(c.device.SampleRate())
}

func (c *CaptureDevice) Channels() int {
	return int(c.device.CaptureChannels())
}

func (c *CaptureDevice) Format() FormatType {
	return FormatType(c.device.CaptureFormat())
}
