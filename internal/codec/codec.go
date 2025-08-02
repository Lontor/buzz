package codec

type SlicePool interface {
	Get() []byte
	Put([]byte)
}
