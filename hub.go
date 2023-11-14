package main

import (
	"context"
	"fmt"
)

type Hub struct {
	subChannel   chan *WSOP
	topicChannel chan string
	subbedTopics map[string]*Topic
}

func NewHub() *Hub {
	return &Hub{
		subChannel:   make(chan *WSOP, 5),
		topicChannel: make(chan string),
		subbedTopics: make(map[string]*Topic, 300),
	}
}

func (h *Hub) runHub() {
	// starts and monitor Topic goroutine
	// forward subscription messages to Topics
	defer fmt.Println("\nHub stopping...")
	for {
		select {
		case wsop := <-h.subChannel:
			h.handleMessage(wsop)
		case msg := <-h.topicChannel:
			fmt.Println("Removing Topic from subbed Topics")
			h.subbedTopics[msg].closeChannel <- true
			delete(h.subbedTopics, msg)
		}
	}
}

func (h *Hub) handleMessage(wsop *WSOP) {
	// handle subscription message
	switch wsop.Op {
	case "subscribe":
		fmt.Println("Hub received wsop ", wsop)
		for _, ch := range wsop.Args {
			if topic, exists := h.subbedTopics[ch]; exists {
				topic.register <- wsop
				continue
			}
			exists, err := rpool.Exists(context.Background(), ch+"::markets").Result()

			if err != nil || exists == 0 {
				// Hub message to client
				fmt.Println("Topic does not exist", ch)
				wsop.client.outChannel <- []byte("error: Topic does not exist " + ch)
				continue
			}
			// create a Topic and keep topic_id
			new_topic := NewTopic(ch, h)
			h.subbedTopics[ch] = new_topic
			// run topic
			go new_topic.runTopic()
			// and register a connection
			new_topic.register <- wsop
		}
	case "unsubscribe":
		fmt.Println("Hub received wsop ", wsop)
		for _, ch := range wsop.Args {
			// if client actually subscribed to requested topic
			if _, ex := wsop.client.subscriptions[ch]; ex {
				// if such topic actually exists
				if topic, exists := h.subbedTopics[ch]; exists {
					// then topic thread will unregister
					topic.unregister <- wsop
				}
			}
		}
	default:
		return
	}
}
