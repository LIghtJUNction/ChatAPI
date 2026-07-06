package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

type ConfigAutomationRulesHandler struct {
	Config  config.Config
	Service *service.AutomationRuleService
	Audit   *service.AuditService
}

func (h ConfigAutomationRulesHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	rules, err := h.Service.ListRules(r.Context(), userID, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"rules": rules,
	})
}

func (h ConfigAutomationRulesHandler) Post(w http.ResponseWriter, r *http.Request) {
	userID, err := currentActorUserID(r, h.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid automation rules request", http.StatusBadRequest)
		return
	}
	rules, err := mapSlice(body["rules"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nextRules, err := h.Service.ReplaceRules(r.Context(), userID, nil, rules)
	if err != nil {
		if errors.Is(err, service.ErrInvalidAutomationRule) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.recordAudit(r, userID, len(nextRules))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"rules": nextRules,
	})
}

func (h ConfigAutomationRulesHandler) recordAudit(r *http.Request, userID string, ruleCount int) {
	if h.Audit == nil {
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "user.config",
		ResourceType: "automation_rule",
		Action:       "replace",
		Outcome:      "success",
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"user_id":    userID,
			"rule_count": ruleCount,
		},
	})
}
