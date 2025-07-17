package rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sync"

	"github.com/pion/webrtc/v4"
)

type message struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type requestMessage struct {
	Type int
	Err  chan<- error
}

type Signaler struct {
	conn        Conn
	pc          *webrtc.PeerConnection
	ctx         context.Context
	cancel      context.CancelFunc
	iceCh       chan webrtc.ICECandidateInit
	sdpCh       chan webrtc.SessionDescription
	randCh      chan int64
	handshakeCh chan int64
	requestCh   chan requestMessage
	randMu      sync.Mutex
}

func SetSignaler(ctx context.Context, conn Conn, pc *webrtc.PeerConnection) *Signaler {
	ctx, cancel := context.WithCancel(ctx)
	s := &Signaler{
		conn:        conn,
		pc:          pc,
		ctx:         ctx,
		cancel:      cancel,
		iceCh:       make(chan webrtc.ICECandidateInit, 100),
		sdpCh:       make(chan webrtc.SessionDescription, 10),
		randCh:      make(chan int64, 10),
		handshakeCh: make(chan int64, 10),
		requestCh:   make(chan requestMessage, 100),
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			s.sendCandidate(c.ToJSON())
		}
	})

	go s.readLoop()
	go s.iceLoop()
	go s.sdpLoop()
	return s
}

func (s *Signaler) readLoop() {
	defer func() {
		s.Close()
		close(s.iceCh)
		close(s.sdpCh)
		close(s.randCh)
		close(s.handshakeCh)
		close(s.requestCh)
	}()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		data, err := s.conn.Read(s.ctx)
		if err != nil {
			return
		}

		var msg message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "ice":
			var ice webrtc.ICECandidateInit
			if err := json.Unmarshal(msg.Data, &ice); err == nil {
				select {
				case s.iceCh <- ice:
				case <-s.ctx.Done():
					return
				}
			}

		case "sdp":
			var sdp webrtc.SessionDescription
			if err := json.Unmarshal(msg.Data, &sdp); err == nil {
				select {
				case s.sdpCh <- sdp:
				case <-s.ctx.Done():
					return
				}
			}

		case "rand":
			var num int64
			if err := json.Unmarshal(msg.Data, &num); err == nil {
				select {
				case s.randCh <- num:
				case <-s.ctx.Done():
				}
			}

		case "handshake":
			var num int64
			if err := json.Unmarshal(msg.Data, &num); err == nil {
				select {
				case s.handshakeCh <- num:
				case <-s.ctx.Done():
				}
			}
		case "init":
			select {
			case s.requestCh <- requestMessage{Type: Answerer}:
			case <-s.ctx.Done():
			}
		}

	}
}

func (s *Signaler) iceLoop() {
	for {
		select {
		case ice := <-s.iceCh:
			s.pc.AddICECandidate(ice)
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Signaler) ExchangeSDP(ctx context.Context) error {
	done := make(chan error, 1)

	s.requestCh <- requestMessage{Type: Offerer, Err: done}

	select {
	case err := <-done:
		return err
	case <-s.ctx.Done():
		return fmt.Errorf("signaler closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Signaler) sdpLoop() {
	continueCh := make(chan struct{}, 1)
	errChans := make([]chan<- error, 0, 6)

	returnErr := func(err error) {
		for _, ch := range errChans {
			ch <- err
		}
		errChans = errChans[:0]
	}

	for {
		select {
		case req := <-s.requestCh:
			if req.Type == Offerer {
				errChans = append(errChans, req.Err)
			}
			for len(s.requestCh) > 0 {
				req = <-s.requestCh
				if req.Type == Offerer {
					errChans = append(errChans, req.Err)
				}
			}
		case <-continueCh:
			for len(s.requestCh) > 0 {
				req := <-s.requestCh
				if req.Type == Offerer {
					errChans = append(errChans, req.Err)
				}
			}

		case <-s.ctx.Done():
			return
		}

		if len(errChans) == 0 {
			_, err := s.negotiatePriority(true, true)
			if err != nil {
				returnErr(err)
				continue
			}
			s.waitForOffer()
			continue
		}

		data, _ := json.Marshal(message{Type: "init"})
		err := s.conn.Write(s.ctx, data)
		if err != nil {
			returnErr(err)
			continue
		}

		priority, err := s.negotiatePriority(false, true)
		if err != nil {
			returnErr(err)
			continue
		}

		if !priority {
			continueCh <- struct{}{}
			s.waitForOffer()
			continue
		}

		err = s.createOffer()
		returnErr(err)
	}
}

func (s *Signaler) negotiatePriority(setLow bool, useHandshakeMessage bool) (bool, error) {
	ch := s.handshakeCh
	t := "handshake"
	if !useHandshakeMessage {
		ch = s.randCh
		t = "rand"
		s.randMu.Lock()
		defer s.randMu.Unlock()
	}
	for {
		n := int64(-1)
		if !setLow {
			n = rand.Int64()
		}

		nData, _ := json.Marshal(n)
		msg := message{
			Type: t,
			Data: nData,
		}
		data, _ := json.Marshal(msg)

		if err := s.conn.Write(s.ctx, data); err != nil {
			return false, err
		}

		var m int64
		select {
		case m = <-ch:
		case <-s.ctx.Done():
			return false, s.ctx.Err()
		}

		switch {
		case n > m:
			return true, nil
		case n < m:
			return false, nil
		}
	}
}

func (s *Signaler) GetPriority(ctx context.Context) (bool, error) {
	done := make(chan struct{})
	var priority bool
	var err error
	go func() {
		defer close(done)
		priority, err = s.negotiatePriority(false, false)
	}()

	select {
	case <-done:
		return priority, err
	case <-ctx.Done():
		return false, ctx.Err()
	case <-s.ctx.Done():
		return false, fmt.Errorf("signaler closed")
	}
}

func (s *Signaler) createOffer() error {
	offer, err := s.pc.CreateOffer(nil)
	if err != nil {
		return err
	}

	if err := s.pc.SetLocalDescription(offer); err != nil {
		return err
	}

	offerData, err := json.Marshal(offer)
	if err != nil {
		return err
	}

	msg := message{
		Type: "sdp",
		Data: offerData,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	if err := s.conn.Write(s.ctx, data); err != nil {
		return err
	}

	select {
	case answer := <-s.sdpCh:
		return s.pc.SetRemoteDescription(answer)
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *Signaler) waitForOffer() error {
	select {
	case offer := <-s.sdpCh:
		if err := s.pc.SetRemoteDescription(offer); err != nil {
			return err
		}

		answer, err := s.pc.CreateAnswer(nil)
		if err != nil {
			return err
		}

		if err := s.pc.SetLocalDescription(answer); err != nil {
			return err
		}

		answerData, err := json.Marshal(answer)
		if err != nil {
			return err
		}

		msg := message{
			Type: "sdp",
			Data: answerData,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		return s.conn.Write(s.ctx, data)
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *Signaler) sendCandidate(c webrtc.ICECandidateInit) {
	iceData, err := json.Marshal(c)
	if err != nil {
		return
	}
	msg := message{
		Type: "ice",
		Data: iceData,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	_ = s.conn.Write(s.ctx, data)
}

func (s *Signaler) Close() error {
	s.cancel()
	return s.conn.Close()
}
