# Anser SDK for Go

Official Go client for the [Anser](https://github.com/ca-x/anser) v1 notification API.

## Install

Requires Go 1.24 or newer and a running Anser server.

```sh
go get github.com/lib-x/anser-sdk-go@v0.2.0
```

## Authenticate

In the Anser admin console, open **API Keys → Create key**. Grant `send` for sending and reading delivery status, or `subscribe` for subscription management. Save the generated key to a secret manager when Anser displays it.

Set `ANSER_URL` to your installation URL, including any reverse-proxy path prefix and without `/v1`. Set `ANSER_API_KEY` from your secret manager. SDK requests use the `Authorization: Bearer` header. Use HTTPS for remote servers.

A key with a topic prefix can only act on matching topics. Direct sends require a key without a prefix. For the example, create and configure an `alerts` topic in Anser. If it is password-protected, also set `ANSER_TOPIC_PASSWORD`.

## Publish and look up delivery

Copy the example to `main.go` and run `go run .`.

```go
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
```

An HTTP `202` confirms that Anser queued the message. The message can still be pending when you first read it. Save the message ID and check the message detail and its delivery attempts as needed. Use the same key to query a message created with a non-admin key.

## Send with a template

Template methods require Anser 0.6.0 or later. Set either `Publish.TemplateID` or `Publish.TemplateName` and supply `Publish.Variables`. The server supplies the final title, body, and format. The [complete example](examples/template/main.go) uses `builtin.deployment` and omits the template source. Use the same `ANSER_IDEMPOTENCY_KEY` if you retry across processes.

The SDK also lists, reads, creates, updates, and deletes templates. API keys with `send` can use built-in and shared templates. Private templates require the owner’s session, and creating a shared template requires a global administrator. See [the template guide](https://github.com/ca-x/anser/blob/main/docs/notification-templates.en.md).

## Send a voice notification

Use `SendSpeech` with a global administrator credential and an OpenAI-compatible speech configuration. The server renders an MP3 and sends it to an inbox, topic, or default route. Topic and channel voice delivery requires a reachable HTTPS `ANSER_PUBLIC_URL`. See [speech plugin options](https://github.com/ca-x/anser/tree/main/anser-plugins/openai-tts).

## API coverage

`Send`, `Publish`, `Message`, `Messages`, `Subscribe`, `Unsubscribe`. Direct send uses explicit selectors or the server's default route. Messages are paginated by cursor. A protected topic uses `topic_password` on publish and `password` on subscribe. Subscription targets tied to channel accounts require a global administrator.

Pass a `context.Context` to cancel or bound requests. For caller-managed send retries, reuse `SendOptions.IdempotencyKey`; the SDK does not schedule retries. Use `errors.As` with `*anser.APIError` to inspect HTTP status, API code, and Retry-After. Server idempotency defaults to a 24-hour window per key.

[Anser documentation](https://ca-x.github.io/anser/?lang=en#sdk) · [简体中文使用说明](https://ca-x.github.io/anser/?lang=zh-CN#sdk)
