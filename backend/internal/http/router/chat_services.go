package router

import (
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	catalogsvc "github.com/zyf2007/ChatAPI/internal/service/chat/catalog"
	ingresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/ingress"
	preprocesssvc "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess"
	streamingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/streaming"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
)

func firstCatalog(current *catalogsvc.Service, modelKeys *modelkey.Service) *catalogsvc.Service {
	if current != nil {
		return current
	}
	return catalogsvc.New(modelKeys)
}

func firstIngress(current *ingresssvc.Service, preprocess *preprocesssvc.Service, turn *turnsvc.Service) *ingresssvc.Service {
	if current != nil {
		return current
	}
	return ingresssvc.New(preprocess, turn)
}

func firstStreaming(current *streamingsvc.Service) *streamingsvc.Service {
	if current != nil {
		return current
	}
	return streamingsvc.New()
}

func firstTimeline(current *timelinesvc.Service, store chat.Store) *timelinesvc.Service {
	if current != nil {
		return current
	}
	return timelinesvc.New(store, nil)
}
