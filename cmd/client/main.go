package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"unsafe"

	"github.com/gen2brain/malgo"
	"github.com/hraban/opus"
)

func newAudioHandler(inCh, outCh chan []byte) func([]byte, []byte, uint32) {
	buf := bytes.NewBuffer(make([]byte, 1024))
	var tmp []byte
	return func(out, in []byte, framecount uint32) {
	loop:
		for buf.Len() < int(framecount)*2 {
			select {
			case tmp = <-inCh:
				buf.Write(tmp)
			default:
				break loop
			}
		}

		if buf.Len() >= int(framecount)*2 {
			io.ReadFull(buf, out)
		}

		tmp := make([]byte, len(in))
		copy(tmp, in)
		select {
		case outCh <- tmp:
		default:

		}
	}
}

func bytesToOpus(encoder *opus.Encoder, inCh, outCh chan []byte) {
	const frameSize = 960
	const channels = 1

	buf := bytes.NewBuffer(nil)

	for in := range inCh {
		buf.Write(in)

		for buf.Len() >= frameSize*2 {
			frame := make([]int16, frameSize)
			binary.Read(buf, binary.LittleEndian, frame)

			encoded := make([]byte, 400)
			n, _ := encoder.Encode(frame, encoded)
			outCh <- encoded[:n]
		}
	}
}

func opusToBytes(decoder *opus.Decoder, inCh, outCh chan []byte) {
	const frameSize = 960

	for in := range inCh {
		pcm := make([]int16, frameSize)
		n, _ := decoder.Decode(in, pcm)
		outCh <- unsafe.Slice((*byte)(unsafe.Pointer(&pcm[0])), n*2)
	}
}

func main() {
	audioFormat := malgo.FormatS16
	channelsNum := 1
	sampleRate := 48000

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {})
	if err != nil {
		log.Fatalln(err)
	}
	defer func() {
		ctx.Uninit()
		ctx.Free()
	}()

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Duplex)
	deviceConfig.Capture.Format = audioFormat
	deviceConfig.Capture.Channels = uint32(channelsNum)
	deviceConfig.Playback.Format = audioFormat
	deviceConfig.Playback.Channels = uint32(channelsNum)
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.Alsa.NoMMap = 1

	playbackCh := make(chan []byte, 16)
	mikeCh := make(chan []byte, 16)

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{Data: newAudioHandler(playbackCh, mikeCh)})
	if err != nil {
		log.Fatalln(err)
	}

	encodedCh := make(chan []byte, 16)
	encoder, _ := opus.NewEncoder(sampleRate, channelsNum, opus.AppAudio)
	encoder.SetBitrateToMax()
	encoder.SetComplexity(10)
	go bytesToOpus(encoder, mikeCh, encodedCh)

	decoder, _ := opus.NewDecoder(sampleRate, channelsNum)
	go opusToBytes(decoder, encodedCh, playbackCh)

	err = device.Start()

	select {}
}
