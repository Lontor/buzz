package audio

import (
	"bytes"
	"io"
)

type FrameBuffer struct {
	buf           bytes.Buffer
	frameSize     int
	maxBufferSize int
}

func NewFrameBuffer(frameSize, maxBufferSize int) *FrameBuffer {
	return &FrameBuffer{
		frameSize:     frameSize,
		maxBufferSize: maxBufferSize,
	}
}

func (f *FrameBuffer) Write(data []byte) {
	if len(data) > f.maxBufferSize {
		data = data[len(data)-f.maxBufferSize:]
	}

	if f.buf.Len()+len(data) > f.maxBufferSize {
		excessData := f.buf.Len() + len(data) - f.maxBufferSize
		f.buf.Next(excessData)
	}
	f.buf.Write(data)
}

func (f *FrameBuffer) ReadFrame() []byte {
	if f.buf.Len() < f.frameSize {
		return nil
	}
	out := make([]byte, f.frameSize)
	_, _ = io.ReadFull(&f.buf, out)
	return out
}

