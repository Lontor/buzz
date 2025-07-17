package rtc

import (
	"context"
	"sync"

	"github.com/pion/webrtc/v4"
)

const (
	Unknown = iota
	Offerer
	Answerer
)

type Conn interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Close() error
}

type PeerConnection struct {
	pc       *webrtc.PeerConnection
	signaler *Signaler
	ctx      context.Context
	cancel   func()
}

func Dial(ctx context.Context, pc *webrtc.PeerConnection, conn Conn) (*PeerConnection, error) {
	ct, cancel := context.WithCancel(context.Background())
	protocol := "signaling"
	var err error

	peer := &PeerConnection{
		pc:     pc,
		ctx:    ct,
		cancel: cancel,
	}

	var dcConn *DataChannelConn
	waitCh := make(chan struct{})
	var once sync.Once
	wait := func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			once.Do(func() { close(waitCh) })
		})
		if dc.ReadyState() == webrtc.DataChannelStateOpen {
			once.Do(func() { close(waitCh) })
		}
	}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dcConn = NewConn(dc)
		wait(dc)
	})

	signaler := SetSignaler(ctx, conn, pc)
	priority, err := signaler.GetPriority(ctx)
	if err != nil {
		return nil, err
	}

	if priority {
		dc, err := pc.CreateDataChannel("signaling", &webrtc.DataChannelInit{Protocol: &protocol})
		if err != nil {
			return nil, err
		}
		dcConn = NewConn(dc)
		wait(dc)

		err = signaler.ExchangeSDP(ctx)
		if err != nil {
			return nil, err
		}
	}

	select {
	case <-waitCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	signaler.Close()

	pc.OnDataChannel(nil)
	peer.signaler = SetSignaler(ct, dcConn, pc)

	pc.OnNegotiationNeeded(func() {
		peer.signaler.ExchangeSDP(peer.ctx)
	})

	return peer, nil
}

func (p *PeerConnection) Close() error {
	p.cancel()
	return p.pc.Close()
}
