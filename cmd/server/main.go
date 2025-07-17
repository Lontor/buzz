package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/pion/turn/v4"
)

func serveTURN() {
	log.Println("Starting TURN server...")
	usersMap := map[string][]byte{}
	usersMap["user"] = turn.GenerateAuthKey("user", "pion.ly", "pass")

	udpListener, err := net.ListenPacket("udp4", ":3478")
	if err != nil {
		log.Fatalf("Failed to create TURN server listener: %s", err)
	}

	log.Println("TURN server listener created successfully.")

	server, err := turn.NewServer(turn.ServerConfig{
		Realm: "pion.ly",
		AuthHandler: func(username string, realm string, srcAddr net.Addr) ([]byte, bool) {
			if key, ok := usersMap[username]; ok {
				return key, true
			}

			return nil, false
		},
		PacketConnConfigs: []turn.PacketConnConfig{
			{
				PacketConn: udpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
					RelayAddress: net.ParseIP("185.120.59.142"),
					Address:      "0.0.0.0",
				},
			},
		},
	})
	if err != nil {
		log.Panic(err)
	}

	log.Println("TURN server started successfully.")

	server.AllocationCount()
}

type Room struct {
	Mu   sync.Mutex
	Num  int
	Conn [2]*websocket.Conn
}

func (r *Room) relayMessages(ctx context.Context) {
	errCh := make(chan error)
	relay := func(src, dst *websocket.Conn) {
		for {
			msgType, data, err := src.Read(ctx)
			if err != nil {
				errCh <- err
			}

			if err := dst.Write(ctx, msgType, data); err != nil {
				errCh <- err
			}
		}
	}

	go relay(r.Conn[0], r.Conn[1])
	go relay(r.Conn[1], r.Conn[0])

	select {
	case <-ctx.Done():
	case <-errCh:
	}
}

func main() {
	go serveTURN()

	cache := make(map[int]*Room, 100)
	var mu sync.Mutex

	go func() {
		http.HandleFunc("/signaling", func(w http.ResponseWriter, r *http.Request) {
			peer, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}

			ctx := context.Background()
			var roomNum int
			wsjson.Read(ctx, peer, &roomNum)

			mu.Lock()
			room, ok := cache[roomNum]

			if !ok {
				cache[roomNum] = &Room{Conn: [2]*websocket.Conn{peer, nil}, Num: 1}
				mu.Unlock()
				return
			}
			mu.Unlock()

			room.Mu.Lock()
			if room.Num > 1 {
				peer.Close(websocket.StatusTryAgainLater, "full room")
				room.Mu.Unlock()
				return
			}
			room.Num++
			room.Conn[1] = peer
			room.Mu.Unlock()

			defer func() {
				mu.Lock()
				delete(cache, roomNum)
				mu.Unlock()
			}()
			ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
			room.relayMessages(ctx)
			cancel()
		})

		err := http.ListenAndServe(":8080", nil)
		if err != nil {
			log.Fatalln(err)
		}
	}()

	select {}
}
