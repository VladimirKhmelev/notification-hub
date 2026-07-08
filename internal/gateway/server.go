package gateway

import (
	"context"
	"log"
	"net/http"

	"nhooyr.io/websocket"
)

type Server struct {
	hub *hub
}

func NewServer(ctx context.Context, databaseURL string) *Server {
	h := newHub()
	go listenNotify(ctx, databaseURL, h)
	return &Server{hub: h}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // allow any origin on localhost
	})
	if err != nil {
		log.Printf("ws-gateway: accept: %v", err)
		return
	}
	defer conn.CloseNow()

	ch, unsub := s.hub.subscribe()
	defer unsub()

	ctx := conn.CloseRead(r.Context())
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		}
	}
}
