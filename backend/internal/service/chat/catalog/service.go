package catalog

import (
	"context"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
)

type ModelKeyStore interface {
	ListModelsByUser(context.Context, string) ([]common.VirtualModel, error)
}

type Service struct {
	modelKeys ModelKeyStore
}

type ModelDescriptor struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func New(modelKeys ModelKeyStore) *Service {
	return &Service{modelKeys: modelKeys}
}

func (s *Service) ListModelsForPrincipal(ctx context.Context) ([]ModelDescriptor, error) {
	if s == nil || s.modelKeys == nil {
		return nil, nil
	}
	principal, ok := modelkey.PrincipalFromContext(ctx)
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		return nil, nil
	}

	items, err := s.modelKeys.ListModelsByUser(ctx, principal.UserID)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	models := make([]ModelDescriptor, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		models = append(models, ModelDescriptor{
			ID:      name,
			Object:  "model",
			Created: item.CreatedAt.Unix(),
			OwnedBy: "chatapi",
		})
	}
	return models, nil
}
