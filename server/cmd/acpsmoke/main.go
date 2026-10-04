// Command acpsmoke is a manual smoke test for the sandbox ACP client.
// Usage: acpsmoke <port> "<prompt>"
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: acpsmoke <port> <prompt>")
		os.Exit(2)
	}
	var port int
	_, _ = fmt.Sscanf(os.Args[1], "%d", &port)
	prompt := os.Args[2]

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	if err := sandbox.WaitForACPReady(ctx, "127.0.0.1", port, "", 60*time.Second); err != nil {
		fmt.Println("not ready:", err)
		os.Exit(1)
	}
	c := sandbox.NewACPClient("127.0.0.1", port).WithSession("/root/workspace", nil)
	if err := c.Connect(ctx); err != nil {
		fmt.Println("connect:", err)
		os.Exit(1)
	}
	defer c.Close()
	fmt.Println("connected session:", c.SessionID())

	res, err := c.ChatStructured(ctx, prompt, nil)
	if err != nil {
		fmt.Println("chat err:", err)
		os.Exit(1)
	}
	fmt.Println("=== narration ===")
	fmt.Println(res.Narration)
	fmt.Printf("=== tools=%d plan=%v ===\n", len(res.ToolCalls), res.Plan != nil)
}
