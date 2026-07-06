package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

const virtualModelsConfigKey = "virtual_models"

var ErrInvalidVirtualModel = errors.New("invalid virtual model")

type VirtualModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	OwnedBy string `json:"owned_by"`
	Created int64  `json:"created"`
	Enabled bool   `json:"enabled"`
}

type VirtualModelSchema struct {
	CreateFields []VirtualModelFieldSpec `json:"create_fields"`
}

type VirtualModelFieldSpec struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type VirtualModelService struct {
	store store.Store
}

func NewVirtualModelService(dataStore store.Store) *VirtualModelService {
	return &VirtualModelService{store: dataStore}
}

func (s *VirtualModelService) Schema() VirtualModelSchema {
	return VirtualModelSchema{
		CreateFields: []VirtualModelFieldSpec{
			{Name: "id", Required: true, Description: "Virtual model identifier exposed to external clients."},
			{Name: "name", Required: true, Description: "Display name for the virtual model."},
			{Name: "owned_by", Required: false, Description: "Owner label returned by model listing endpoints."},
			{Name: "created", Required: false, Description: "Unix timestamp returned by model listing endpoints."},
			{Name: "enabled", Required: false, Description: "Whether the virtual model is visible in /models responses."},
		},
	}
}

func (s *VirtualModelService) List(ctx context.Context, userID string) ([]VirtualModel, error) {
	if s == nil || s.store == nil {
		return nil, ErrInvalidVirtualModel
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, ErrInvalidVirtualModel
	}
	item, err := s.store.GetUserConfig(ctx, userID, virtualModelsConfigKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return defaultVirtualModels(), nil
		}
		return nil, err
	}
	models, err := virtualModelsFromConfig(item.Value)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return defaultVirtualModels(), nil
	}
	return models, nil
}

func (s *VirtualModelService) Upsert(ctx context.Context, userID string, input VirtualModel) ([]VirtualModel, error) {
	if s == nil || s.store == nil {
		return nil, ErrInvalidVirtualModel
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, ErrInvalidVirtualModel
	}
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.OwnedBy = strings.TrimSpace(input.OwnedBy)
	if input.ID == "" || input.Name == "" {
		return nil, ErrInvalidVirtualModel
	}
	if input.OwnedBy == "" {
		input.OwnedBy = "chatapi"
	}
	if input.Created <= 0 {
		input.Created = time.Now().UTC().Unix()
	}

	models, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	replaced := false
	for index := range models {
		if models[index].ID != input.ID {
			continue
		}
		models[index] = input
		replaced = true
		break
	}
	if !replaced {
		models = append(models, input)
	}
	if err := s.persist(ctx, userID, models); err != nil {
		return nil, err
	}
	return s.List(ctx, userID)
}

func (s *VirtualModelService) Delete(ctx context.Context, userID string, modelID string) ([]VirtualModel, error) {
	if s == nil || s.store == nil {
		return nil, ErrInvalidVirtualModel
	}
	userID = strings.TrimSpace(userID)
	modelID = strings.TrimSpace(modelID)
	if userID == "" || modelID == "" {
		return nil, ErrInvalidVirtualModel
	}
	models, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	filtered := make([]VirtualModel, 0, len(models))
	found := false
	for _, item := range models {
		if item.ID == modelID {
			found = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !found {
		return nil, store.ErrNotFound
	}
	if err := s.persist(ctx, userID, filtered); err != nil {
		return nil, err
	}
	return s.List(ctx, userID)
}

func (s *VirtualModelService) OpenAIList(ctx context.Context, userID string) ([]map[string]any, error) {
	models, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(models))
	for _, item := range models {
		if !item.Enabled {
			continue
		}
		items = append(items, map[string]any{
			"id":       item.ID,
			"object":   "model",
			"created":  item.Created,
			"owned_by": item.OwnedBy,
		})
	}
	if len(items) == 0 {
		for _, item := range defaultVirtualModels() {
			items = append(items, map[string]any{
				"id":       item.ID,
				"object":   "model",
				"created":  item.Created,
				"owned_by": item.OwnedBy,
			})
		}
	}
	return items, nil
}

func (s *VirtualModelService) persist(ctx context.Context, userID string, models []VirtualModel) error {
	items := make([]map[string]any, 0, len(models))
	for _, item := range models {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" {
			return ErrInvalidVirtualModel
		}
		ownedBy := strings.TrimSpace(item.OwnedBy)
		if ownedBy == "" {
			ownedBy = "chatapi"
		}
		items = append(items, map[string]any{
			"id":       item.ID,
			"name":     item.Name,
			"owned_by": ownedBy,
			"created":  item.Created,
			"enabled":  item.Enabled,
		})
	}
	_, err := s.store.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: userID,
		Key:    virtualModelsConfigKey,
		Value: map[string]any{
			"items": items,
		},
	})
	return err
}

func virtualModelsFromConfig(value map[string]any) ([]VirtualModel, error) {
	rawItems, ok := value["items"]
	if !ok {
		return nil, nil
	}
	list, ok := rawItems.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: items", ErrInvalidVirtualModel)
	}
	models := make([]VirtualModel, 0, len(list))
	for _, rawItem := range list {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: item", ErrInvalidVirtualModel)
		}
		model := VirtualModel{
			ID:      strings.TrimSpace(virtualModelStringValue(item["id"], "")),
			Name:    strings.TrimSpace(virtualModelStringValue(item["name"], "")),
			OwnedBy: strings.TrimSpace(virtualModelStringValue(item["owned_by"], "chatapi")),
			Created: int64(virtualModelNumberValue(item["created"])),
			Enabled: virtualModelBoolValue(item["enabled"], true),
		}
		if model.ID == "" || model.Name == "" {
			return nil, ErrInvalidVirtualModel
		}
		if model.Created <= 0 {
			model.Created = 0
		}
		models = append(models, model)
	}
	return models, nil
}

func defaultVirtualModels() []VirtualModel {
	return []VirtualModel{{
		ID:      "chatapi-lab",
		Name:    "ChatAPI Lab",
		OwnedBy: "chatapi",
		Created: 0,
		Enabled: true,
	}}
}

func virtualModelNumberValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case int32:
		return float64(typed)
	default:
		return 0
	}
}

func virtualModelBoolValue(value any, fallback bool) bool {
	flag, ok := value.(bool)
	if !ok {
		return fallback
	}
	return flag
}

func virtualModelStringValue(value any, fallback string) string {
	raw, ok := value.(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	return strings.TrimSpace(raw)
}
