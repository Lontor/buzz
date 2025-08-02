package rtc

import (
	"context"
	"errors"
	"sync"

	"github.com/pion/webrtc/v4"
)

type DataChannelConn struct {
	dc       *webrtc.DataChannel
	readCh   chan []byte
	closeCh  chan struct{}
	closeErr error
	mu       sync.Mutex
	label    string
	protocol string
}

func createDataChannel(dc *webrtc.DataChannel) <-chan *DataChannelConn {
	dcConn := &DataChannelConn{
		dc:      dc,
		readCh:  make(chan []byte, 64),
		closeCh: make(chan struct{}),
	}

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		select {
		case dcConn.readCh <- msg.Data:
		case <-dcConn.closeCh:
		}
	})

	dc.OnClose(func() {
		dcConn.Close()
	})

	ready := make(chan *DataChannelConn, 1)
	var once sync.Once

	waitOpen := func() {
		once.Do(func() { ready <- dcConn })
	}

	dc.OnOpen(waitOpen)
	if dc.ReadyState() == webrtc.DataChannelStateOpen {
		waitOpen()
	}

	return ready
}

func (ps *PeerSession) NewDataChannel(ctx context.Context, label, protocol string) (*DataChannelConn, error) {
	dc, err := ps.pc.CreateDataChannel(label, &webrtc.DataChannelInit{Protocol: &protocol})
	if err != nil {
		return nil, err
	}

	ready := createDataChannel(dc)

	select {
	case dcConn := <-ready:
		dcConn.label = dc.Label()
		dcConn.protocol = dc.Protocol()
		return dcConn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (ps *PeerSession) AcceptDataChannel(ctx context.Context) (*DataChannelConn, error) {
	var dc *webrtc.DataChannel
	for {
		select {
		case dc = <-ps.incomingDCCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		ready := createDataChannel(dc)

		select {
		case dcConn := <-ready:
			dcConn.label = dc.Label()
			dcConn.protocol = dc.Protocol()
			return dcConn, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}

	}
}

func (c *DataChannelConn) Write(ctx context.Context, data []byte) error {
	done := make(chan error, 1)
	go func() {
		done <- c.dc.Send(data)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closeCh:
		return errors.New("connection closed")
	}
}

func (c *DataChannelConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case data := <-c.readCh:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closeCh:
		c.mu.Lock()
		defer c.mu.Unlock()
		return nil, c.closeErr
	}
}

func (c *DataChannelConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closeErr == nil {
		c.closeErr = errors.New("closed by user")
		close(c.closeCh)
	}
	return c.dc.Close()
}

func (c *DataChannelConn) Laber() string {
	return c.label
}

func (c *DataChannelConn) Protocol() string {
	return c.protocol
}
