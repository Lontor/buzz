package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/pion/turn/v4"
)

var cache sync.Map

func serveTURN() {
	udpListener, err := net.ListenPacket("udp4", "0.0.0.0:3478")
	if err != nil {
		log.Fatalf("Failed to create TURN server listener: %s", err)
	}

	server, err := turn.NewServer(turn.ServerConfig{
		Realm: "pion.ly",
		AuthHandler: func(username string, realm string, srcAddr net.Addr) ([]byte, bool) {
			return []byte("noop"), true
		},
		PacketConnConfigs: []turn.PacketConnConfig{
			{
				PacketConn: udpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
					RelayAddress: net.ParseIP("127.0.0.1"),
					Address:      "0.0.0.0",
				},
			},
		},
	})
	if err != nil {
		log.Panic(err)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	server.Close()
}

func main() {
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

	serveTURN()

	select {}
}
