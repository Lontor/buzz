package rtc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type trackParts struct {
	track    chan *webrtc.TrackRemote
	receiver chan *webrtc.RTPReceiver
	info     TrackInfoInternal
}

type trackRequest struct {
	track    *webrtc.TrackRemote
	receiver *webrtc.RTPReceiver
	info     TrackInfoInternal
}

type TrackManager struct {
	conn          Conn
	pc            *webrtc.PeerConnection
	pendingTracks map[string]trackParts
	tracksCh      chan *TrackReader
	ctx           context.Context
	cancel        func()
}

func (pd *PeerSession) SetTrackManager(conn Conn) *TrackManager {
	ctx, cancel := context.WithCancel(pd.ctx)
	tm := &TrackManager{
		conn:          conn,
		pc:            pd.pc,
		pendingTracks: make(map[string]trackParts, 32),
		tracksCh:      make(chan *TrackReader, 12),
		ctx:           ctx,
		cancel:        cancel,
	}
	go tm.trackAccepteLoop()
	return tm
}

func (tm *TrackManager) trackAccepteLoop() {

	partCh := make(chan trackRequest, 12)
	tm.pc.OnTrack(func(tr *webrtc.TrackRemote, r *webrtc.RTPReceiver) {
		partCh <- trackRequest{track: tr, receiver: r}
	})

	go func() {
		defer tm.cancel()
		var info TrackInfoInternal
		for {
			data, err := tm.conn.Read(tm.ctx)
			if err != nil {
				return
			}
			err = json.Unmarshal(data, &info)
			if err != nil {
				return
			}
			partCh <- trackRequest{info: info}
		}
	}()

	var req trackRequest
	for {
		select {
		case req = <-partCh:
		case <-tm.ctx.Done():
			return
		}

		if req.track == nil {
			id := req.info.TrackID
			part, ok := tm.pendingTracks[id]
			if ok {
				tm.tracksCh <- tm.createTrackReader(part.track, part.receiver, req.info)
				delete(tm.pendingTracks, id)
			} else {
				trCh := make(chan *webrtc.TrackRemote, 1)
				rcCh := make(chan *webrtc.RTPReceiver, 1)
				tm.pendingTracks[id] = trackParts{
					track:    trCh,
					receiver: rcCh,
					info:     req.info,
				}
				tm.tracksCh <- tm.createTrackReader(trCh, rcCh, req.info)
			}
		} else {
			id := req.track.ID()
			part, ok := tm.pendingTracks[id]
			if ok {
				part.track <- req.track
				part.receiver <- req.receiver
				delete(tm.pendingTracks, id)
			} else {
				trCh := make(chan *webrtc.TrackRemote, 1)
				trCh <- req.track
				rcCh := make(chan *webrtc.RTPReceiver, 1)
				rcCh <- req.receiver
				tm.pendingTracks[id] = trackParts{track: trCh, receiver: rcCh}
			}
		}
	}
}

type TrackInfo struct {
	Kind           string
	MimeType       string
	ClockRate      uint32
	Channels       uint16
	StreamID       string
	TrackName      string
	SampleDuration time.Duration
	BitDepth       uint8
}

type TrackInfoInternal struct {
	TrackInfo
	TrackID string `json:"trackId"`
}

type TrackWriter struct {
	track  *webrtc.TrackLocalStaticSample
	sender *webrtc.RTPSender
	info   TrackInfoInternal
	ctx    context.Context
	cancel context.CancelFunc
}

func (tm *TrackManager) NewTrack(ctx context.Context, info TrackInfo) (*TrackWriter, error) {
	fullInfo := TrackInfoInternal{TrackInfo: info}
	id := uuid.New().String()
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  info.MimeType,
			ClockRate: info.ClockRate,
			Channels:  info.Channels,
		},
		id, info.StreamID,
	)

	fullInfo.TrackID = id

	if err != nil {
		return nil, err
	}

	sender, err := tm.pc.AddTrack(track)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(fullInfo)
	if err != nil {
		return nil, err
	}

	if err := tm.conn.Write(ctx, data); err != nil {
		return nil, err
	}

	trackCtx, cancel := context.WithCancel(tm.ctx)

	return &TrackWriter{
		track:  track,
		sender: sender,
		info:   fullInfo,
		ctx:    trackCtx,
		cancel: cancel,
	}, nil
}

func (tw *TrackWriter) Write(ctx context.Context, data []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tw.ctx.Done():
		return tw.ctx.Err()
	default:
		return tw.track.WriteSample(media.Sample{
			Data:     data,
			Duration: tw.info.SampleDuration,
		})
	}
}

func (tw *TrackWriter) Info() TrackInfo { return tw.info.TrackInfo }

func (tw *TrackWriter) Close() error {
	tw.cancel()
	return nil
}

type TrackReader struct {
	track    *webrtc.TrackRemote
	receiver *webrtc.RTPReceiver
	info     TrackInfoInternal
	readCh   chan *rtp.Packet
	ctx      context.Context
	cancel   context.CancelFunc
}

func (tm *TrackManager) AcceptTrack(ctx context.Context) (*TrackReader, error) {
	select {
	case track := <-tm.tracksCh:
		return track, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (tm *TrackManager) createTrackReader(trackCh <-chan *webrtc.TrackRemote, receiverCh <-chan *webrtc.RTPReceiver, info TrackInfoInternal) *TrackReader {

	trackCtx, cancel := context.WithCancel(tm.ctx)

	rd := &TrackReader{
		info:   info,
		readCh: make(chan *rtp.Packet, 20),
		ctx:    trackCtx,
		cancel: cancel,
	}

	go func() {
		select {
		case <-tm.ctx.Done():
			return
		case rd.track = <-trackCh:
		}

		select {
		case <-tm.ctx.Done():
			return
		case rd.receiver = <-receiverCh:
		}
		rd.readLoop()
	}()

	return rd
}

func (tr *TrackReader) readLoop() {
	defer tr.Close()

	for {
		select {
		case <-tr.ctx.Done():
			return
		default:
			pkt, _, err := tr.track.ReadRTP()
			if err != nil {
				return
			}

			select {
			case tr.readCh <- pkt:
			case <-tr.ctx.Done():
				return
			}
		}
	}
}

func (tr *TrackReader) ReadRTP(ctx context.Context) (*rtp.Packet, error) {
	select {
	case pkt := <-tr.readCh:
		return pkt, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-tr.ctx.Done():
		return nil, tr.ctx.Err()
	}
}

func (tr *TrackReader) Info() TrackInfo { return tr.info.TrackInfo }

func (tr *TrackReader) Close() error {
	tr.cancel()
	return nil
}
