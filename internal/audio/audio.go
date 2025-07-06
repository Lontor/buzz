package audio

type FormatType uint8

const (
	FormatUnknown FormatType = iota
	FormatU8
	FormatS16
	FormatS24
	FormatS32
	FormatF32
)

type HandlerFunc func(data []byte, frameCount int)

func FormatSize(format FormatType) int {
	if format < 4 {
		return int(format)
	}
	return 4
}
