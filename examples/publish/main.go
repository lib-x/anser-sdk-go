package main

import (
	"context"
	"fmt"
	"log"
	"os"

	anser "github.com/lib-x/anser-sdk-go"
)

func main() {
	client, err := anser.New(anser.Config{
		ServerURL: os.Getenv("ANSER_URL"),
		APIKey:    os.Getenv("ANSER_API_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	receipt, err := client.Publish(ctx, "alerts", anser.Publish{
		Message:       anser.Message{Title: "Build complete", Body: "The release is ready."},
		TopicPassword: os.Getenv("ANSER_TOPIC_PASSWORD"),
	}, anser.SendOptions{})
	if err != nil {
		log.Fatal(err)
	}
	detail, err := client.Message(ctx, receipt.MessageID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %s\n", receipt.MessageID, detail.Status)
}
