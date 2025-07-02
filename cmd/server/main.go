package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/pion/turn/v4"
)

var cache sync.Map

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

func main() {
	go serveTURN()

	go func() {
		http.HandleFunc("/signaling", func(w http.ResponseWriter, r *http.Request) {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			ctx := context.Background()
			var roomNum int
			wsjson.Read(ctx, conn, &roomNum)
			if cl, ok := cache.LoadOrStore(roomNum, conn); ok {
				fmt.Println("Start")
				cli := (cl).(*websocket.Conn)
				wsjson.Write(ctx, cli, "offer")
				wsjson.Write(ctx, conn, "answer")
				t, data, _ := cli.Read(ctx)
				conn.Write(ctx, t, data)
				fmt.Println("offer send")
				t, data, _ = conn.Read(ctx)
				cli.Write(ctx, t, data)
				fmt.Println("answer send")
				cli.CloseNow()
				conn.CloseNow()
				cache.Delete(roomNum)
			}
		})

		err := http.ListenAndServe(":8080", nil)
		if err != nil {
			log.Fatalln(err)
		}
	}()

	select {}
}
