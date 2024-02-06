package main

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/contrib/websocket"
)

type WSClient struct {
	conn          *websocket.Conn
	hub           *Hub
	subscriptions map[string]bool
	outChannel    chan []byte
	register      chan string
	unregister    chan string
	close         chan bool
}

func NewWSClient(ws *websocket.Conn, h *Hub) *WSClient {
	return &WSClient{
		conn:          ws,
		hub:           h,
		subscriptions: make(map[string]bool),
		outChannel:    make(chan []byte, 10),
		register:      make(chan string, 20),
		unregister:    make(chan string, 20),
		close:         make(chan bool, 1),
	}
}

func (wsc *WSClient) RunWS() {
	go wsc.ReadMsg()
	defer func() {
		wsc.CloseWS()
	}()
	for {
		select {
		case outMsg := <-wsc.outChannel:
			if err := wsc.conn.WriteMessage(1, outMsg); err != nil {
				return
			}
		case topic_id := <-wsc.register:
			wsc.subscriptions[topic_id] = true
		case topic_id := <-wsc.unregister:
			// fmt.Println("C")
			delete(wsc.subscriptions, topic_id)
		case <-wsc.close:
			return
		}
	}
}

func (wsc *WSClient) ReadMsg() {
	for {
		mt, msg, err := wsc.conn.ReadMessage()
		if err != nil {
			wsc.close <- true
			return
		}
		if ok := wsc.HandleMessage(mt, msg); !ok {
			wsc.close <- true
			return
		}
	}
}

func (wsc *WSClient) CloseWS() {
	// unsub from every topic
	keys := make([]string, 0, len(wsc.subscriptions))
	for key := range wsc.subscriptions {
		keys = append(keys, key)
	}
	wsop := WSOP{
		Op:     "unsubscribe",
		Args:   keys,
		client: wsc,
	}
	// send unsubscribe to hub
	wsc.hub.subChannel <- &wsop
}

func (wsc *WSClient) HandleMessage(mt int, msg []byte) bool {
	// returns bool whether to keep socket alive
	switch mt {
	case 1:
		// accept subscription messages
		// and create subscribe op to send to hub and then forward to topic
		var temp struct {
			Op   string `json:"op"`
			Args string `json:"args"`
		}
		if err := json.Unmarshal([]byte(msg), &temp); err != nil {
			wsc.outChannel <- []byte(`{"error":"Message format"}`)
			return true
		}
		arr := strings.FieldsFunc(temp.Args, func(r rune) bool {
			return r == ','
		})

		wsop := WSOP{
			Op:     temp.Op,
			Args:   arr,
			client: wsc,
		}
		// forward subscription message to Hub
		wsc.hub.subChannel <- &wsop
		return true
	case 8:
		// close message
		return false
	case 9:
		// ping message
		wsc.conn.WriteControl(10, []byte("pong"), time.Now().Add(1000000000*60*5))
		return true
	default:
		return true
	}
}
