package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/custom/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestCompleteRetriesServerErrorsWithFreshRequestBody(t *testing.T) {
	attempts := 0
	client := NewClient(config.LLMConfig{
		BaseURL: "https://llm.example.test/v1", APIKey: "key", Model: "model",
		TimeoutSeconds: 10, PromptVersion: "prompt-v1",
	})
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if !strings.Contains(string(body), `"model":"model"`) ||
			!strings.Contains(string(body), `"temperature":0`) ||
			!strings.Contains(string(body), `"response_format":{"type":"json_object"}`) {
			t.Fatalf("request body = %s", body)
		}
		if attempts < 3 {
			return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("temporary")), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"{\"content\":\"ok\"}"}}]}`)), Header: make(http.Header)}, nil
	})

	content, err := client.Complete(t.Context(), "prompt")
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if content != `{"content":"ok"}` || attempts != 3 {
		t.Fatalf("content=%q attempts=%d", content, attempts)
	}
}

func TestClientExposesModelAndPromptVersion(t *testing.T) {
	client := NewClient(config.LLMConfig{Model: "model", PromptVersion: "prompt-v2"})
	if client.Model() != "model" || client.PromptVersion() != "prompt-v2" {
		t.Fatalf("model=%q prompt_version=%q", client.Model(), client.PromptVersion())
	}
}

func TestCompleteWithSystemUsesSeparateSystemAndUserMessages(t *testing.T) {
	client := NewClient(config.LLMConfig{BaseURL: "https://llm.example.test/v1", APIKey: "key", Model: "model"})
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var payload struct {
			Messages []Message `json:"messages"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(payload.Messages) != 2 || payload.Messages[0].Role != "system" || payload.Messages[1].Role != "user" {
			t.Fatalf("messages = %+v", payload.Messages)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{}"}}]}`)), Header: make(http.Header)}, nil
	})
	content, err := client.CompleteWithSystem(t.Context(), "system", "user")
	if err != nil || content != "{}" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestStreamEmitsOpenAICompatibleDeltas(t *testing.T) {
	client := NewClient(config.LLMConfig{BaseURL: "https://llm.example.test/v1", APIKey: "key", Model: "model"})
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `"stream":true`) {
			t.Fatalf("request body = %s", body)
		}
		stream := "data: " + `{"choices":[{"delta":{"content":"{"}}]}` + "\n\n" + "data: " + `{"choices":[{"delta":{"content":"}"}}]}` + "\n\n" + "data: [DONE]\n\n"
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(stream))}, nil
	})
	var deltas []string
	content, err := client.Stream(t.Context(), "prompt", func(delta string) error { deltas = append(deltas, delta); return nil })
	if err != nil || content != "{}" || strings.Join(deltas, "") != "{}" {
		t.Fatalf("content=%q deltas=%v err=%v", content, deltas, err)
	}
}

func TestStreamUsesCallerDeadlineInsteadOfClientTotalTimeout(t *testing.T) {
	client := NewClient(config.LLMConfig{BaseURL: "https://llm.example.test/v1", APIKey: "key", Model: "model", TimeoutSeconds: 1})
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) < 4*time.Second {
			t.Fatalf("request deadline was shortened to the client timeout: deadline=%v ok=%v", deadline, ok)
		}
		stream := "data: " + `{"choices":[{"delta":{"content":"{}"}}]}` + "\n\n" + "data: [DONE]\n\n"
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(stream))}, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	content, err := client.Stream(ctx, "prompt", nil)
	if err != nil || content != "{}" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}
