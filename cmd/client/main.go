package main

import (
	"context"
	"log"
	"time"

	"buzz/internal/audio"
	"buzz/internal/codec"
	"buzz/internal/transport/rtc"
	"buzz/internal/transport/ws"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/pion/webrtc/v4"
	"golang.org/x/sync/errgroup"
)

func main() {
	const audioFormat = audio.FormatF32
	const channelsNum = 2
	const sampleRate = 48000

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Println("Connecting to WebSocket server...")
	conn, _, err := websocket.Dial(ctx, "ws://192.168.1.60:8080/signaling", nil)
	if err != nil {
		log.Fatalf("Error dialing WebSocket: %v", err)
	}

	log.Println("Setting up PeerConnection...")
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Fatalf("Error creating PeerConnection: %v", err)
	}

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("ICE connection state changed: %s", state)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("PeerConnection state changed: %s", state)
	})

	pc.OnSignalingStateChange(func(ss webrtc.SignalingState) {
		log.Printf("Signaling state changed: %s", ss)
	})

	log.Println("Sending WebSocket message...")
	err = wsjson.Write(ctx, conn, 256)
	if err != nil {
		log.Fatalf("Error sending WebSocket message: %v", err)
	}

	log.Println("Dialing RTC...")
	peer, err := rtc.Dial(ctx, pc, ws.NewConn(conn))
	if err != nil {
		log.Fatalf("Error dialing RTC: %v", err)
	}

	log.Println("Creating audio track for sending...")
	send, err := peer.NewTrack(ctx, rtc.TrackInfo{
		Kind:           "audio",
		MimeType:       webrtc.MimeTypeOpus,
		ClockRate:      48000,
		Channels:       2,
		StreamID:       "stream",
		TrackName:      "stream",
		SampleDuration: 20 * time.Millisecond,
		BitDepth:       32,
	})
	if err != nil {
		log.Fatalf("Error creating send track: %v", err)
	}

	log.Println("Accepting audio track for receiving...")
	receive, err := peer.AcceptTrack(ctx)
	if err != nil {
		log.Fatalf("Error accepting receive track: %v", err)
	}

	log.Println("Initializing audio engine...")
	eng, err := audio.NewAudioEngine()
	if err != nil {
		log.Fatalf("Error initializing audio engine: %v", err)
	}
	defer func() {
		log.Println("Closing audio engine...")
		eng.Close()
	}()

	log.Println("Creating capture device...")
	capt, err := eng.NewCaptureDevice(ctx, audioFormat, channelsNum, sampleRate, 20)
	if err != nil {
		log.Fatalf("Error creating capture device: %v", err)
	}
	defer func() {
		log.Println("Closing capture device...")
		capt.Close()
	}()

	log.Println("Creating playback device...")
	play, err := eng.NewPlaybackDevice(ctx, audioFormat, channelsNum, sampleRate, 20)
	if err != nil {
		log.Fatalf("Error creating playback device: %v", err)
	}
	defer func() {
		log.Println("Closing playback device...")
		play.Close()
	}()

	log.Println("Creating Opus decoder...")
	dec, err := codec.NewOpusDecoder(audioFormat, channelsNum, sampleRate, 20)
	if err != nil {
		log.Fatalf("Error creating Opus decoder: %v", err)
	}

	log.Println("Creating Opus encoder...")
	enc, err := codec.NewOpusEncoder(audioFormat, channelsNum, sampleRate)
	if err != nil {
		log.Fatalf("Error creating Opus encoder: %v", err)
	}

	enc.SetBitrateToMax()
	enc.SetComplexity(10)
	enc.SetDTX(true)

	log.Println("Starting capture, encode, decode, and playback...")

	group, groupCtx := errgroup.WithContext(ctx)

	captureCh := make(chan []byte, 16)
	compressCh := make(chan []byte, 16)
	receiveCh := make(chan codec.DecodeTask, 16)
	decompressCh := make(chan []byte, 16)

	group.Go(func() error {
		stage, err := capt.CaptureStage(groupCtx, captureCh)
		if err != nil {
			return err
		}
		return stage()
	})

	group.Go(enc.EncodeStage(groupCtx, captureCh, compressCh))

	group.Go(func() error {
		var data []byte
		for {
			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			case data = <-compressCh:
			}

			err := send.Write(groupCtx, data)
			if err != nil {
				return err
			}
		}
	})

	group.Go(func() error {
		stage, err := play.PlaybackStage(groupCtx, decompressCh)
		if err != nil {
			return err
		}
		return stage()
	})

	group.Go(dec.DecodeStage(groupCtx, receiveCh, decompressCh))

	group.Go(func() error {
		for {
			pack, err := receive.ReadRTP(groupCtx)
			if err != nil {
				return err
			}

			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			case receiveCh <- codec.DecodeTask{Data: pack.Payload, RestoreFrame: false}:
			}
		}
	})

	log.Fatal(group.Wait())
}
