package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"unsafe"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gen2brain/malgo"
	"github.com/hraban/opus"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
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

	malgoCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {})
	if err != nil {
		log.Fatalln(err)
	}
	defer func() {
		malgoCtx.Uninit()
		malgoCtx.Free()
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

	device, err := malgo.InitDevice(malgoCtx.Context, deviceConfig, malgo.DeviceCallbacks{Data: newAudioHandler(playbackCh, mikeCh)})
	if err != nil {
		log.Fatalln(err)
	}

	encodedCh := make(chan []byte, 16)
	encoder, _ := opus.NewEncoder(sampleRate, channelsNum, opus.AppAudio)
	encoder.SetBitrateToMax()
	encoder.SetComplexity(10)
	go bytesToOpus(encoder, mikeCh, encodedCh)

	decoder, _ := opus.NewDecoder(sampleRate, channelsNum)
	decodedCh := make(chan []byte, 16)
	go opusToBytes(decoder, decodedCh, playbackCh)

	err = device.Start()

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs:       []string{"stun:stun.l.google.com:19302"},
				Username:   "user",
				Credential: "pass",
			},
		},
	}

	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Fatalln(err)
	}

	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "audio")
	if err != nil {
		log.Fatalln(err)
	}

	_, err = peerConnection.AddTrack(track)
	if err != nil {
		log.Fatalln(err)
	}

	go func() {
		seq := uint16(0)
		ssrc := uint32(12345)
		timestamp := uint32(0)
		for {
			audioData := <-encodedCh

			packet := &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					PayloadType:    111,
					SequenceNumber: seq,
					Timestamp:      timestamp,
					SSRC:           ssrc,
				},
				Payload: audioData,
			}

			seq++
			timestamp += 960

			err := track.WriteRTP(packet)
			if err != nil {
				log.Println("Error writing RTP packet:", err)
			}
		}
	}()

	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go func() {
			for {
				packet, _, err := track.ReadRTP()
				if err != nil {
					log.Println("Error reading RTP packet:", err)
					return
				}
				decodedAudio := make([]byte, len(packet.Payload))
				copy(decodedAudio, packet.Payload)
				decodedCh <- decodedAudio
			}
		}()
	})

	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		fmt.Println("ICE state:", state)
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		fmt.Println("PeerConnection state:", state)
	})

	ctx := context.Background()
	ws, _, err := websocket.Dial(ctx, "ws://0.0.0.0:8080/signaling", nil)
	if err != nil {
		log.Fatalln(err)
	}

	roomID := 123
	wsjson.Write(ctx, ws, roomID)

	var role string
	wsjson.Read(ctx, ws, &role)

	if role == "offer" {
		offer, err := peerConnection.CreateOffer(nil)
		if err != nil {
			log.Fatalln(err)
		}
		peerConnection.SetLocalDescription(offer)

		<-webrtc.GatheringCompletePromise(peerConnection)

		wsjson.Write(ctx, ws, peerConnection.LocalDescription())
		var answer webrtc.SessionDescription
		wsjson.Read(ctx, ws, &answer)
		peerConnection.SetRemoteDescription(answer)
	} else {
		var offer webrtc.SessionDescription
		wsjson.Read(ctx, ws, &offer)
		peerConnection.SetRemoteDescription(offer)

		answer, err := peerConnection.CreateAnswer(nil)
		if err != nil {
			log.Fatalln(err)
		}
		peerConnection.SetLocalDescription(answer)

		<-webrtc.GatheringCompletePromise(peerConnection)

		wsjson.Write(ctx, ws, peerConnection.LocalDescription())
	}

	select {}
}
