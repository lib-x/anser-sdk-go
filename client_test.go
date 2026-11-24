package anser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNotificationFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /proxy/v1/topics/{topic}/publish", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
			body["topic_password"] != "topic-pass" || body["title"] != "Hello" ||
			r.PathValue("topic") != "alerts.test" || r.Header.Get("Idempotency-Key") != "build-42" ||
			r.URL.Query().Get("timeout_ms") != "4000" {
			t.Errorf("unexpected publish request: %s", r.URL)
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"message_id":"m1","status":"pending"}`)
	})
	mux.HandleFunc("POST /proxy/v1/send", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Channels []Target `json:"channels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Channels) != 1 || body.Channels[0].Channel != "bark" {
			t.Error("missing direct routing target")
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"message_id":"m1","status":"pending"}`)
	})
	mux.HandleFunc("GET /proxy/v1/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"m1","status":"success","deliveries":[{"attempts":[{"n":1}]}]}`)
	})
	mux.HandleFunc("GET /proxy/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") != "a+b" || r.URL.Query().Get("limit") != "5" {
			t.Error("invalid pagination")
		}
		fmt.Fprint(w, `{"items":[],"next_cursor":"next"}`)
	})
	mux.HandleFunc("POST /proxy/v1/topics/{topic}/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		var body SubscriptionInput
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
			body.SubscriberRef != "integration" || body.Password != "topic-pass" {
			t.Error("invalid subscription")
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"a/b","subscriber_ref":"integration","filter":[],"created_at":1}`)
	})
	mux.HandleFunc("DELETE /proxy/v1/topics/{topic}/subscriptions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "a/b" || !strings.HasSuffix(r.URL.EscapedPath(), "/a%2Fb") {
			t.Error("ID was not encoded as one segment")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing API key")
		}
		mux.ServeHTTP(w, r)
	}))
	defer server.Close()
	client, err := New(Config{ServerURL: server.URL + "/proxy/", APIKey: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	receipt, err := client.Publish(ctx, "alerts.test", Publish{Message: Message{Title: "Hello"}, TopicPassword: "topic-pass"},
		SendOptions{IdempotencyKey: "build-42", Wait: true, WaitTimeout: 4 * time.Second})
	if err != nil || receipt.MessageID != "m1" {
		t.Fatalf("publish: %+v, %v", receipt, err)
	}
	_, err = client.Send(ctx, Publish{Message: Message{Title: "Hello"}, Targets: []Target{{Channel: "bark"}}}, SendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := client.Message(ctx, receipt.MessageID)
	if err != nil || detail.Status != "success" || len(detail.Deliveries) != 1 || len(detail.Deliveries[0].Attempts) != 1 {
		t.Fatalf("message: %+v, %v", detail, err)
	}
	page, err := client.Messages(ctx, PageOptions{Cursor: "a+b", Limit: 5})
	if err != nil || page.NextCursor == nil || *page.NextCursor != "next" {
		t.Fatalf("page: %+v, %v", page, err)
	}
	sub, err := client.Subscribe(ctx, "alerts.test", SubscriptionInput{SubscriberRef: "integration", Password: "topic-pass"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Unsubscribe(ctx, "alerts.test", sub.ID); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPErrorAndRedirect(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/redirect") {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"code":"rate_limited","message":"Limit exceeded","details":{"limit":5}}`)
	}))
	defer server.Close()
	client, err := New(Config{ServerURL: server.URL, APIKey: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	var api *APIError
	_, err = client.Message(t.Context(), "limited")
	if !errors.As(err, &api) || api.Status != 429 || api.Code != "rate_limited" || api.RetryAfter != "5" {
		t.Fatalf("rate limit: %v", err)
	}
	_, err = client.Message(t.Context(), "redirect")
	if !errors.As(err, &api) || api.Status != 307 || redirected.Load() {
		t.Fatalf("redirect: %v, destination hit: %v", err, redirected.Load())
	}
	if strings.Contains(fmt.Sprintf("%#v", client), "fixture") {
		t.Fatal("client exposes key")
	}
}

func TestTemplateAPI(t *testing.T) {
	const definition = `{"id":"custom-id","name":"deploy","format":"text","variables":["service"]}`
	var called atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/send":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode: %v", err)
			}
			if body["template_id"] != "builtin.deployment" || body["title"] != nil || body["format"] != nil {
				t.Errorf("invalid template request: %+v", body)
			}
			if value, ok := body["variables"].(map[string]any); !ok || value["service"] != "api" {
				t.Errorf("missing variables: %+v", body)
			}
			w.WriteHeader(202)
			fmt.Fprint(w, `{"message_id":"m1","status":"pending"}`)
		case "GET /v1/templates":
			fmt.Fprint(w, `{"items":[`+definition+`],"next_cursor":null}`)
		case "POST /v1/templates", "GET /v1/templates/custom-id", "PUT /v1/templates/custom-id":
			fmt.Fprint(w, definition)
		case "DELETE /v1/templates/custom-id":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := New(Config{ServerURL: server.URL, APIKey: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	_, err = client.Send(ctx, Publish{TemplateID: "builtin.deployment", Variables: map[string]string{"service": "api"}}, SendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Send(ctx, Publish{TemplateID: "one", TemplateName: "two"}, SendOptions{})
	if err == nil {
		t.Fatal("accepted two references")
	}
	page, err := client.Templates(ctx, PageOptions{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("templates: %+v, %v", page, err)
	}
	input := TemplateInput{Name: "deploy", Body: "{{service}}", Format: Text}
	out, err := client.CreateTemplate(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Template(ctx, out.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = client.UpdateTemplate(ctx, out.ID, input); err != nil {
		t.Fatal(err)
	}
	if err = client.DeleteTemplate(ctx, out.ID); err != nil {
		t.Fatal(err)
	}
	if called.Load() != 6 {
		t.Fatalf("expected six requests, got %d", called.Load())
	}
}

func TestSendSpeech(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input SpeechSend
		if r.Method != "POST" || r.URL.Path != "/v1/speech/send" ||
			r.Header.Get("Idempotency-Key") != "speech-17" ||
			json.NewDecoder(r.Body).Decode(&input) != nil ||
			input.Destination.Kind != "inbox" || input.Destination.Username != "alice" {
			t.Error("unexpected speech request")
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"message_id":"m1","status":"success"}`)
	}))
	defer server.Close()
	client, err := New(Config{ServerURL: server.URL, APIKey: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	input := SpeechSend{
		Config:      SpeechConfig{ServerURL: "https://speech.example", APIKey: "fixture-provider"},
		Destination: SpeechDestination{Kind: "inbox", Username: "alice"},
		Message:     Message{Body: "Your build is ready"},
	}
	receipt, err := client.SendSpeech(t.Context(), input, "speech-17")
	if err != nil || receipt.MessageID != "m1" {
		t.Fatalf("send speech: %+v, %v", receipt, err)
	}
	if strings.Contains(fmt.Sprintf("%#v", input), "fixture-provider") {
		t.Fatal("provider key in diagnostics")
	}
}
