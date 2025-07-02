package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

var cache sync.Map

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

	select {}
}
