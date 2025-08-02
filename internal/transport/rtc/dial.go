package rtc

import (
	"context"
	"fmt"

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

type PeerSession struct {
	pc       *webrtc.PeerConnection
	signaler *Signaler
	*TrackManager
	ctx    context.Context
	cancel func()

	incomingDCCh chan *webrtc.DataChannel
}

func Dial(ctx context.Context, pc *webrtc.PeerConnection, conn Conn) (*PeerSession, error) {
	ct, cancel := context.WithCancel(ctx)

	peer := &PeerSession{
		pc:           pc,
		ctx:          ct,
		cancel:       cancel,
		incomingDCCh: make(chan *webrtc.DataChannel, 12),
	}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		peer.incomingDCCh <- dc
	})

	signaler := SetSignaler(ct, conn, pc)
	priority, err := signaler.GetPriority(ct)
	if err != nil {
		return nil, err
	}

	pc.OnNegotiationNeeded(func() {
		_ = signaler.ExchangeSDP(peer.ctx)
	})

	var dcConn *DataChannelConn
	if priority {
		dcConn, err = peer.NewDataChannel(ct, "init", "signaler")
		if err != nil {
			return nil, err
		}
	} else {
		dcConn, err = peer.AcceptDataChannel(ct)
		if err != nil {
			return nil, err
		}
	}

	signaler.Close()
	peer.signaler = SetSignaler(ct, dcConn, pc)

	pc.OnNegotiationNeeded(func() {
		err = peer.signaler.ExchangeSDP(peer.ctx)
		if err != nil {
			fmt.Println(err)
		}
	})

	var metaDC *DataChannelConn
	if priority {
		metaDC, err = peer.NewDataChannel(ct, "track-metadata", "metadata")
		if err != nil {
			return nil, err
		}
	} else {
		metaDC, err = peer.AcceptDataChannel(ct)
		if err != nil {
			return nil, err
		}
	}
	peer.TrackManager = peer.SetTrackManager(metaDC)

	return peer, nil
}

func (tm *PeerSession) Close() error {
	tm.cancel()
	return tm.pc.Close()
}
