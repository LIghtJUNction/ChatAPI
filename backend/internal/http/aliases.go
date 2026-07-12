package httpapi

import (
	"net/http"

	httphandler "github.com/zyf2007/ChatAPI/internal/http/handler"
	httprouter "github.com/zyf2007/ChatAPI/internal/http/router"
)

type RouterDeps = httprouter.Deps

type ChatAPIHandler = httphandler.ChatAPIHandler
type AppAPIHandler = httphandler.AppAPIHandler
type AuthHandler = httphandler.AuthHandler
type UserHandler = httphandler.UserHandler
type AdminHandler = httphandler.AdminHandler
type LabHandler = httphandler.LabHandler
type HealthHandler = httphandler.HealthHandler
type ReadinessHandler = httphandler.ReadinessHandler
type MetricsHandler = httphandler.MetricsHandler
type SetupHandler = httphandler.SetupHandler

func NewRouter(deps RouterDeps) http.Handler {
	return httprouter.New(deps)
}
