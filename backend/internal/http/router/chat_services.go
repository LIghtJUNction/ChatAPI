package router

import (
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	catalogsvc "github.com/zyf2007/ChatAPI/internal/service/chat/catalog"
	egresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/egress"
	ingresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/ingress"
	streamingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/streaming"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

func firstCatalog(current *catalogsvc.Service, modelKeys *modelkey.Service) *catalogsvc.Service {
	if current != nil {
		return current
	}
	return catalogsvc.New(modelKeys)
}

func firstIngress(current *ingresssvc.Service, turn *turnsvc.Service) *ingresssvc.Service {
	if current != nil {
		return current
	}
	return ingresssvc.New(turn)
}

func firstStreaming(current *streamingsvc.Service) *streamingsvc.Service {
	if current != nil {
		return current
	}
	return streamingsvc.New()
}

func firstEgress(current *egresssvc.Service) *egresssvc.Service {
	if current != nil {
		return current
	}
	return egresssvc.New()
}

func firstTimeline(current *timelinesvc.Service, store chat.Store) *timelinesvc.Service {
	if current != nil {
		return current
	}
	return timelinesvc.New(store, nil)
}
