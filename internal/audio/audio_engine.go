package audio

import (
	"github.com/gen2brain/malgo"
)

type AudioEngine struct {
	ctx  *malgo.AllocatedContext
	pool SlicePool
}

func NewAudioEngine() (*AudioEngine, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
	})
	if err != nil {
		return nil, err
	}

	return &AudioEngine{
		ctx: ctx,
	}, nil
}

func (e *AudioEngine) Close() {
	e.ctx.Uninit()
	e.ctx.Free()
}
