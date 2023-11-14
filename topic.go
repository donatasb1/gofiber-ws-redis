package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Topic struct {
	hub          *Hub
	topic_id     string
	stream_id    map[string]*string
	subs         map[*WSClient]bool
	register     chan *WSOP
	unregister   chan *WSOP
	closeChannel chan bool
	outChannel   chan []byte
	msgCache     map[string]*MsgCache
}

type MsgCache struct {
	cacheSize int
	messages  [][]byte
	msgIndex  int
}

func NewCache(size int) *MsgCache {
	return &MsgCache{
		cacheSize: size,
		messages:  make([][]byte, size),
		msgIndex:  0,
	}
}

func (c *MsgCache) AppendCache(msg_bytes []byte) {
	c.messages[c.msgIndex] = msg_bytes
	c.msgIndex = (c.msgIndex + 1) % c.cacheSize
}

func NewTopic(topic_id string, hub *Hub) *Topic {
	null_id := "0"
	stream_id := map[string]*string{
		topic_id + "::public_trades": &null_id,
		topic_id + "::public_bid":    &null_id,
		topic_id + "::public_ask":    &null_id,
	}

	cache := map[string]*MsgCache{
		topic_id + "::public_trades": NewCache(30),
		topic_id + "::public_bid":    NewCache(1),
		topic_id + "::public_ask":    NewCache(1),
	}
	return &Topic{
		hub:          hub,
		topic_id:     topic_id,
		stream_id:    stream_id,
		subs:         make(map[*WSClient]bool, 200),
		register:     make(chan *WSOP, 10),
		unregister:   make(chan *WSOP, 20),
		closeChannel: make(chan bool, 1),
		outChannel:   make(chan []byte, 30),
		msgCache:     cache,
	}
}

type SuccessMsg struct {
	Success       string   `json:"success"`
	Subscriptions []string `json:"subscriptions"`
}

func (t *Topic) runTopic() {
	defer fmt.Println("Closing topic...")
	fmt.Println("\nTopic starting: ", t.topic_id)
	// Run topic broadcast
	// on init setup ids
	t.initTopicCache()
	go t.ReadStream()

	for {
		select {
		case wsop := <-t.register:

			t.subs[wsop.client] = true
			wsop.client.register <- t.topic_id
			t.SubAck(wsop)
			t.sendCacheMsgs(wsop.client)
		case wsop := <-t.unregister:
			delete(t.subs, wsop.client)
			wsop.client.unregister <- t.topic_id
			if len(t.subs) < 1 {
				// inform hub of Topic closing
				t.hub.topicChannel <- t.topic_id
			}
		case <-t.closeChannel:
			return

		case msg_bytes := <-t.outChannel:
			for client := range t.subs {
				client.outChannel <- msg_bytes
			}

			// for _, stream := range XStream {
			// 	for _, msg := range stream.Messages {
			// 		t.stream_id[stream.Stream] = &msg.ID
			// 		msg_bytes, err := json.Marshal(msg.Values)
			// 		if err != nil {
			// 			fmt.Println("Error:", err)
			// 			continue
			// 		}
			// 		t.msgCache[stream.Stream].AppendCache(msg_bytes)
			// 		for client := range t.subs {
			// 			client.outChannel <- msg_bytes
			// 		}
			// 	}
			// }
		}
	}
}

func (t *Topic) SubAck(wsop *WSOP) {
	ack := SuccessMsg{
		Success:       "true",
		Subscriptions: wsop.Args,
	}
	ack_bytes, err := json.Marshal(ack)
	if err != nil {
		return
	}
	wsop.client.outChannel <- ack_bytes
}

func (t *Topic) StreamArgs() []string {
	return []string{
		t.topic_id + "::public_trades",
		t.topic_id + "::public_bid",
		t.topic_id + "::public_ask",
		*t.stream_id[t.topic_id+"::public_trades"],
		*t.stream_id[t.topic_id+"::public_bid"],
		*t.stream_id[t.topic_id+"::public_ask"],
	}
}

func (t *Topic) ReadStream() {
	for {
		args := redis.XReadArgs{
			Streams: t.StreamArgs(),
			Count:   1,
			Block:   time.Duration(1000000000 * 60 * 5),
		}
		XStream, err := rpool.XRead(context.Background(), &args).Result()
		if err != nil {
			fmt.Println("Empty read", t.topic_id)
			continue
		}
		for _, stream := range XStream {
			for _, msg := range stream.Messages {
				t.stream_id[stream.Stream] = &msg.ID
				msg_bytes, err := json.Marshal(msg.Values)
				if err != nil {
					fmt.Println("Error:", err)
					continue
				}
				t.msgCache[stream.Stream].AppendCache(msg_bytes)
				t.outChannel <- msg_bytes
			}
		}
		// t.outChannel <- XStream
	}
}

func (t *Topic) initTopicCache() {
	for key := range t.stream_id {
		t.fetchStartMsg(key, int64(t.msgCache[key].cacheSize))
		t.GetID(key)
	}
}

func (t *Topic) sendCacheMsgs(wsc *WSClient) {
	for stream_id := range t.stream_id {
		idx := t.msgCache[stream_id].msgIndex
		size := t.msgCache[stream_id].cacheSize
		for i := 0; i < len(t.msgCache[stream_id].messages); i++ {
			msg := t.msgCache[stream_id].messages[int64(idx)]
			idx = (idx + 1) % size
			if msg != nil {
				wsc.outChannel <- msg
			}
		}
	}
}

func (t *Topic) GetID(stream string) {
	// get XInfoStream message from redis
	stream_info, err := rpool.XInfoStream(context.Background(), stream).Result()
	null_id := "0"
	if err != nil {
		t.stream_id[stream] = &null_id
		return
	}
	t.stream_id[stream] = &stream_info.LastEntry.ID
	fmt.Println("set topic id ", t.stream_id)
}

func (t *Topic) fetchStartMsg(stream string, size int64) {
	msgs, err := rpool.XRevRangeN(context.Background(), stream, "+", "-", size).Result()
	if err != nil {
		fmt.Println("Start msg error", err)
		return
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg_bytes, err := json.Marshal(msgs[i].Values)
		if err != nil {
			return
		}
		t.msgCache[stream].AppendCache(msg_bytes)
	}
}
