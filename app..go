package main

import (
	"fmt"
	"strings"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

// gofiber websocket server
// Starts a goroutine for unique topic
// which broadcasts latest messages from redis streams to subscribers
// Allows for single connection to subscribe to many topics

type WSOP struct {
	// incoming ws op message format
	Op     string    `json:"op"`   // subscribe, unsubscribe
	Args   []string  `json:"args"` // topic_id(s)
	client *WSClient // op sender
}

var rpool *redis.Client = redis.NewClient(&redis.Options{
	Addr:     "",
	Password: "",
	DB:       0,
})

func main() {
	// init wm struct
	// Fiber instance
	hub := NewHub()
	go hub.runHub()
	app := fiber.New()
	defer rpool.Close()

	app.Use("/ws", func(c *fiber.Ctx) error {
		// IsWebSocketUpgrade returns true if the client
		// requested upgrade to the WebSocket protocol.
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	app.Get("/ws/:channel", websocket.New(func(c *websocket.Conn) {
		// event_id,event_id,...
		channel_id := c.Params("channel")
		arr := strings.FieldsFunc(channel_id, func(r rune) bool {
			return r == ','
		})

		wsc := NewWSClient(c, hub)
		fmt.Println("Websocket starting. Channel args", arr)
		wsop := &WSOP{
			Op:     "subscribe",
			Args:   arr,
			client: wsc,
		}
		hub.subChannel <- wsop
		wsc.RunWS()
	}))
	// start server
	app.Listen(":3080")
}
