package gateway

import (
	"context"
	"errors"
	"time"

	"systemsfoundry/platform"
)

var ErrStreamInterrupted = errors.New("model stream failed after the first token")

type Request struct {
	TenantID  string
	Prompt    string
	MaxTokens int
	OnToken   func(token string)
}

type StreamingModel interface {
	Model
	Stream(ctx context.Context, prompt string, maxTokens int, emit func(token string)) (string, int, error)
}

type Completion struct {
	Text     string
	Model    string
	Tokens   int
	CacheHit bool
}

type Usage struct {
	TenantID string
	Model    string
	Tokens   int
	CacheHit bool
}

type Recorder interface {
	Record(usage Usage)
}

type Model interface {
	Name() string
	Complete(ctx context.Context, prompt string, maxTokens int) (string, int, error)
}

type StatusError struct {
	Status int
}

func (e *StatusError) Error() string {
	if e == nil {
		return "model error"
	}
	return "model status"
}

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type hashEmbedder struct{}

func (hashEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return platform.HashEmbed(text), nil
}

type Proxy struct {
	embedder     Embedder
	primary      Model
	cheap        Model
	cache        *Cache
	budget       *Budget
	recorder     Recorder
	threshold    float64
	ttl          time.Duration
	modelTimeout time.Duration
	now          func() time.Time
}

func NewProxy(primary, cheap Model, cache *Cache, budget *Budget, recorder Recorder) *Proxy {
	return &Proxy{
		embedder:     hashEmbedder{},
		primary:      primary,
		cheap:        cheap,
		cache:        cache,
		budget:       budget,
		recorder:     recorder,
		threshold:    0.97,
		ttl:          10 * time.Minute,
		modelTimeout: 8 * time.Second,
		now:          time.Now,
	}
}

func (p *Proxy) Complete(ctx context.Context, req Request) platform.Result[Completion] {
	if req.TenantID == "" || req.Prompt == "" || len(req.Prompt) > 32000 {
		return platform.FailFields[Completion]("Invalid prompt.", map[string][]string{
			"prompt": []string{"required"},
		})
	}
	if req.MaxTokens < 1 {
		req.MaxTokens = 256
	}
	vector, embedErr := p.embedder.Embed(ctx, req.Prompt)
	cacheable := embedErr == nil && len(vector) > 0
	if cacheable {
		if hit, ok := p.cache.Find(req.TenantID, vector, p.threshold, p.now()); ok {
			hit.CacheHit = true
			p.record(Usage{TenantID: req.TenantID, Model: hit.Model, Tokens: 0, CacheHit: true})
			return platform.Succeed(hit)
		}
	}
	reserved := EstimateTokens(req.Prompt) + req.MaxTokens
	if !p.budget.Reserve(req.TenantID, reserved) {
		return platform.Fail[Completion](platform.CodeTooMany, "Token budget exceeded.")
	}
	completion, err := p.completeWithFailover(ctx, req)
	if err != nil {
		p.budget.Release(req.TenantID, reserved)
		return platform.Fail[Completion](platform.CodeUnavailable, "Model unavailable.")
	}
	p.budget.Settle(req.TenantID, reserved, completion.Tokens)
	if cacheable {
		p.cache.Save(req.TenantID, vector, completion, p.now().Add(p.ttl), p.now())
	}
	p.record(Usage{TenantID: req.TenantID, Model: completion.Model, Tokens: completion.Tokens, CacheHit: false})
	return platform.Succeed(completion)
}

func (p *Proxy) UseEmbedder(embedder Embedder, threshold float64) {
	p.embedder = embedder
	p.threshold = threshold
}

func (p *Proxy) completeWithFailover(ctx context.Context, req Request) (Completion, error) {
	text, tokens, err := p.call(ctx, p.primary, req)
	if err == nil {
		return Completion{Text: text, Model: p.primary.Name(), Tokens: tokens}, nil
	}
	if !temporary(err) || ctx.Err() != nil {
		return Completion{}, err
	}
	text, tokens, err = p.call(ctx, p.cheap, req)
	if err != nil {
		return Completion{}, err
	}
	return Completion{Text: text, Model: p.cheap.Name(), Tokens: tokens}, nil
}

func (p *Proxy) call(ctx context.Context, model Model, req Request) (string, int, error) {
	if ctx.Err() != nil {
		return "", 0, ctx.Err()
	}
	child, cancel := context.WithTimeout(ctx, p.modelTimeout)
	defer cancel()
	var text string
	var tokens int
	var err error
	streamer, streams := model.(StreamingModel)
	if streams && req.OnToken != nil {
		emitted := false
		text, tokens, err = streamer.Stream(child, req.Prompt, req.MaxTokens, func(token string) {
			emitted = true
			req.OnToken(token)
		})
		if err != nil && emitted {
			return "", 0, ErrStreamInterrupted
		}
	} else {
		text, tokens, err = model.Complete(child, req.Prompt, req.MaxTokens)
	}
	if err == nil {
		return text, tokens, nil
	}
	if ctx.Err() != nil {
		return "", 0, ctx.Err()
	}
	if child.Err() != nil {
		return "", 0, context.DeadlineExceeded
	}
	return "", 0, err
}

func (p *Proxy) record(usage Usage) {
	if p.recorder != nil {
		p.recorder.Record(usage)
	}
}

func temporary(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		switch statusErr.Status {
		case 429, 502, 503, 504:
			return true
		}
	}
	return false
}
