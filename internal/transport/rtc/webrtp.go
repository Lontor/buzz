package rtc

import (
	"sync"

	"github.com/pion/webrtc/v4"
)

type Signaler interface {
	IsOfferer() bool

	SendOffer(offer webrtc.SessionDescription) error
	SendAnswer(answer webrtc.SessionDescription) error

	ReceiveOffer() (webrtc.SessionDescription, error)
	ReceiveAnswer() (webrtc.SessionDescription, error)

	SendICECandidate(candidate *webrtc.ICECandidate) error
	ReceiveICECandidate() (*webrtc.ICECandidateInit, error)
}

type Connection struct {
	conn    *webrtc.PeerConnection
	session *webrtc.DataChannel

	closed    chan struct{}
	closeOnce sync.Once
}

func (c *Connection) Close() error {
	var err error
	c.closeOnce.Do(func() {
		err = c.conn.Close()
		close(c.closed)
	})
	return err
}

func (c *Connection) Done() <-chan struct{} {
	return c.closed
}

func NewConnection(config webrtc.Configuration, signaler Signaler) (*Connection, error) {
	conn, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	connection := &Connection{
		conn:   conn,
		closed: make(chan struct{}),
	}

	conn.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		signaler.SendICECandidate(candidate)
	})

	conn.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateDisconnected,
			webrtc.PeerConnectionStateClosed:
			connection.Close()
		}
	})

	runGathering := func() {
		for {
			select {
			case <-webrtc.GatheringCompletePromise(conn):
				return
			default:
				candidate, err := signaler.ReceiveICECandidate()
				if err != nil {
					continue
				}
				conn.AddICECandidate(*candidate)
			}
		}
	}

	var session *webrtc.DataChannel

	if signaler.IsOfferer() {
		session, err = conn.CreateDataChannel("session", nil)
		if err != nil {
			return nil, err
		}

		offer, err := conn.CreateOffer(nil)
		if err != nil {
			return nil, err
		}
		if err = conn.SetLocalDescription(offer); err != nil {
			return nil, err
		}
		go runGathering()
		if err = signaler.SendOffer(offer); err != nil {
			return nil, err
		}
		answer, err := signaler.ReceiveAnswer()
		if err != nil {
			return nil, err
		}
		if err = conn.SetRemoteDescription(answer); err != nil {
			return nil, err
		}

		connection.session = session
		return connection, nil
	}

	conn.OnDataChannel(func(dc *webrtc.DataChannel) {
		session = dc
		connection.session = dc
	})

	offer, err := signaler.ReceiveOffer()
	if err != nil {
		return nil, err
	}
	if err = conn.SetRemoteDescription(offer); err != nil {
		return nil, err
	}
	answer, err := conn.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}
	if err = conn.SetLocalDescription(answer); err != nil {
		return nil, err
	}
	go runGathering()
	if err = signaler.SendAnswer(answer); err != nil {
		return nil, err
	}

	return connection, nil
}
