package audio

import (
	"errors"

	"github.com/gen2brain/malgo"
)

type PlaybackDevice struct {
	device  *malgo.Device
	handler HandlerFunc
}

func NewPlaybackDevice(ctx malgo.AllocatedContext, format FormatType, channels, sampleRate int) (*PlaybackDevice, error) {
	playback := &PlaybackDevice{}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatType(format)
	deviceConfig.Playback.Channels = uint32(channels)
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.PerformanceProfile = malgo.LowLatency

	callback := func(output, _ []byte, frameCount uint32) {
		if playback.handler != nil {
			playback.handler(output, int(frameCount))
		}
	}

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: callback,
	})
	if err != nil {
		return nil, err
	}
	playback.device = device
	return playback, nil
}

func (p *PlaybackDevice) SetHandler(handler HandlerFunc) error {
	if handler == nil {
		return errors.New("audio handler is not set")
	}
	p.handler = handler
	return nil
}

func (p *PlaybackDevice) Start() error {
	if p.handler == nil {
		return errors.New("audio handler is not set")
	}
	return p.device.Start()
}

func (p *PlaybackDevice) Stop() error {
	return p.device.Stop()
}

func (p *PlaybackDevice) SampleRate() int {
	return int(p.device.SampleRate())
}

func (p *PlaybackDevice) Channels() int {
	return int(p.device.PlaybackChannels())
}

func (p *PlaybackDevice) Format() FormatType {
	return FormatType(p.device.PlaybackFormat())
}
