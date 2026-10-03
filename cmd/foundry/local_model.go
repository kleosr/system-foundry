package main

import (
	"context"
	"strings"
	"time"

	"systemsfoundry/gateway"
)

type echoModel struct {
	name string
	gap  time.Duration
}

func (m echoModel) Name() string { return m.name }

func (m echoModel) Complete(_ context.Context, prompt string, _ int) (string, int, error) {
	text := m.name + ": " + prompt
	return text, gateway.EstimateTokens(prompt) + gateway.EstimateTokens(text), nil
}

func (m echoModel) Stream(ctx context.Context, prompt string, maxTokens int, emit func(string)) (string, int, error) {
	text, tokens, err := m.Complete(ctx, prompt, maxTokens)
	if err != nil {
		return "", 0, err
	}
	for i, word := range strings.Fields(text) {
		if i > 0 {
			word = " " + word
		}
		timer := time.NewTimer(m.gap)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", 0, ctx.Err()
		case <-timer.C:
		}
		emit(word)
	}
	return text, tokens, nil
}
