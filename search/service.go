package search

import (
	"context"
	"errors"

	"systemsfoundry/auth"
	"systemsfoundry/platform"
)

type FoundPage struct {
	IDs    []string `json:"ids"`
	Cursor string   `json:"cursor,omitempty"`
}

const rankDepth = 200

type Repo interface {
	Save(ctx context.Context, actor platform.Actor, doc Document, vector []float32) error
	Rank(ctx context.Context, actor platform.Actor, terms []string, vector []float32, depth int) (lexical, semantic []string, err error)
}

type Service struct {
	repo Repo
}

func NewService(repo Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) Index(ctx context.Context, actor platform.Actor, id, title, body string) platform.Result[Document] {
	fields := map[string][]string{}
	if id == "" {
		fields["id"] = []string{"required"}
	}
	if title == "" {
		fields["title"] = []string{"required"}
	}
	if len(fields) > 0 {
		return platform.FailFields[Document]("Invalid document.", fields)
	}
	if !auth.Can(actor, auth.ActionWrite, actor.TenantID) {
		return platform.Fail[Document](platform.CodeForbidden, "Write is not allowed.")
	}
	doc := Analyze(id, title, body)
	vector := platform.HashEmbed(title + " " + body)
	if err := s.repo.Save(ctx, actor, doc, vector); err != nil {
		if errors.Is(err, ErrCatalogFull) {
			return platform.Fail[Document](platform.CodeUnavailable, "Catalog is full.")
		}
		return platform.Fail[Document](platform.CodeUnavailable, "Catalog is unavailable.")
	}
	return platform.Succeed(doc)
}

func (s *Service) Find(ctx context.Context, actor platform.Actor, query, cursor string, limit int) platform.Result[FoundPage] {
	if query == "" {
		return platform.FailFields[FoundPage]("Invalid query.", map[string][]string{"query": []string{"required"}})
	}
	if !auth.Can(actor, auth.ActionRead, actor.TenantID) {
		return platform.Fail[FoundPage](platform.CodeForbidden, "Read is not allowed.")
	}
	lexical, semantic, err := s.repo.Rank(ctx, actor, Tokens(query), platform.HashEmbed(query), rankDepth)
	if err != nil {
		return platform.Fail[FoundPage](platform.CodeUnavailable, "Catalog is unavailable.")
	}
	fused := Fuse([][]string{lexical, semantic}, 60)
	ids, next := Page(fused, cursor, limit)
	return platform.Succeed(FoundPage{IDs: ids, Cursor: next})
}
