package admincontrol

import "context"

func (s *Service) GetAuthSettings(ctx context.Context) (map[string]any, error) {
	if s.authSettings == nil {
		return nil, errNotConfigured("auth settings")
	}
	return s.authSettings.Get(ctx)
}

func (s *Service) SetAuthSettings(ctx context.Context, body map[string]any) (map[string]any, error) {
	if s.authSettings == nil {
		return nil, errNotConfigured("auth settings")
	}
	return s.authSettings.Set(ctx, body)
}

func (s *Service) GetAccessSettings(ctx context.Context) (map[string]any, error) {
	if s.accessSettings == nil {
		return nil, errNotConfigured("access settings")
	}
	return s.accessSettings.GetDocument(ctx)
}

func (s *Service) SetAccessSettings(ctx context.Context, body map[string]any) (map[string]any, error) {
	if s.accessSettings == nil {
		return nil, errNotConfigured("access settings")
	}
	current, err := s.accessSettings.Set(ctx, body)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":               true,
		"key":              "system_access_settings",
		"current":          current,
		"schema":           s.accessSettings.Schema(),
		"response_version": 1,
	}, nil
}
