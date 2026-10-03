package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

var errEmptyReply = errors.New("provider returned no content")

type OpenAICompatible struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReply struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type streamRequest struct {
	chatRequest
	Stream        bool          `json:"stream"`
	StreamOptions streamOptions `json:"stream_options"`
}

type streamChunk struct {
	Choices []struct {
		Delta chatMessage `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type embeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embeddingReply struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (o *OpenAICompatible) Name() string {
	return o.Model
}

func (o *OpenAICompatible) Complete(ctx context.Context, prompt string, maxTokens int) (string, int, error) {
	var reply chatReply
	request := chatRequest{Model: o.Model, Messages: []chatMessage{{Role: "user", Content: prompt}}, MaxTokens: maxTokens}
	if err := o.post(ctx, "/chat/completions", request, &reply); err != nil {
		return "", 0, err
	}
	if len(reply.Choices) == 0 {
		return "", 0, errEmptyReply
	}
	tokens := reply.Usage.TotalTokens
	if tokens == 0 {
		tokens = EstimateTokens(prompt) + EstimateTokens(reply.Choices[0].Message.Content)
	}
	return reply.Choices[0].Message.Content, tokens, nil
}

func (o *OpenAICompatible) Stream(ctx context.Context, prompt string, maxTokens int, emit func(string)) (string, int, error) {
	request := streamRequest{
		chatRequest:   chatRequest{Model: o.Model, Messages: []chatMessage{{Role: "user", Content: prompt}}, MaxTokens: maxTokens},
		Stream:        true,
		StreamOptions: streamOptions{IncludeUsage: true},
	}
	resp, err := o.send(ctx, "/chat/completions", request)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	var text strings.Builder
	tokens := 0
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok || data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err = json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", 0, err
		}
		if chunk.Usage != nil {
			tokens = chunk.Usage.TotalTokens
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				text.WriteString(choice.Delta.Content)
				emit(choice.Delta.Content)
			}
		}
	}
	if err = scanner.Err(); err != nil {
		return "", 0, err
	}
	if text.Len() == 0 {
		return "", 0, errEmptyReply
	}
	if tokens == 0 {
		tokens = EstimateTokens(prompt) + EstimateTokens(text.String())
	}
	return text.String(), tokens, nil
}

func (o *OpenAICompatible) Embed(ctx context.Context, text string) ([]float32, error) {
	var reply embeddingReply
	if err := o.post(ctx, "/embeddings", embeddingRequest{Model: o.Model, Input: text}, &reply); err != nil {
		return nil, err
	}
	if len(reply.Data) == 0 || len(reply.Data[0].Embedding) == 0 {
		return nil, errEmptyReply
	}
	return reply.Data[0].Embedding, nil
}

func (o *OpenAICompatible) post(ctx context.Context, path string, body, out any) error {
	resp, err := o.send(ctx, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

func (o *OpenAICompatible) send(ctx context.Context, path string, body any) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.BaseURL, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return nil, &StatusError{Status: resp.StatusCode}
	}
	return resp, nil
}
