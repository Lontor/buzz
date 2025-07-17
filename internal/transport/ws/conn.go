package ws

import (
	"context"

	"github.com/coder/websocket"
)

type Conn struct {
	conn *websocket.Conn
}

func NewConn(conn *websocket.Conn) *Conn {
	return &Conn{conn: conn}
}

func (c *Conn) Write(ctx context.Context, data []byte) error {
	return c.conn.Write(ctx, websocket.MessageBinary, data)
}

func (c *Conn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := c.conn.Read(ctx)
	return data, err
}

func (c *Conn) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "closed")
}
