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
}

func NewConn(dc *webrtc.DataChannel) *DataChannelConn {
	c := &DataChannelConn{
		dc:      dc,
		readCh:  make(chan []byte, 64),
		closeCh: make(chan struct{}),
	}

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		select {
		case c.readCh <- msg.Data:
		case <-c.closeCh:

		}
	})

	dc.OnClose(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.closeErr == nil {
			c.closeErr = errors.New("datachannel closed")
		}
		close(c.closeCh)
	})

	return c
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
