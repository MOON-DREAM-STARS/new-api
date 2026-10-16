package service

import (
	"errors"
	"math/rand"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// ServiceTierRoute is the routing state of one request whose model is
// served by channel aggregation. It decides every attempt's channel from the
// tier chain, keeps the channels that failed in this request out of later
// attempts and records the decision trail for the logs.
type ServiceTierRoute struct {
	Model         string
	FamilyID      int
	FamilyName    string
	PolicyScope   string
	PolicyID      int
	PolicyVersion int64
	Preference    string
	PreferredTier string
	Steps         []ServiceTierStep

	strategy     model.AggregationStrategy
	usingGroup   string
	startedAt    time.Time
	stepIndex    int
	stepAttempts []int
	attempts     int
	failed       map[int]bool
	trail        []ServiceTierAttempt
}

// ServiceTierAttempt is one entry of the admin decision trail.
type ServiceTierAttempt struct {
	Attempt     int     `json:"attempt"`
	Tier        string  `json:"tier"`
	Ratio       float64 `json:"ratio"`
	Escalation  bool    `json:"escalation,omitempty"`
	ChannelID   int     `json:"channel_id"`
	ChannelName string  `json:"channel_name,omitempty"`
	Outcome     string  `json:"outcome"`
	Status      int     `json:"status,omitempty"`
	ErrorCode   string  `json:"error_code,omitempty"`
	Category    string  `json:"category,omitempty"`
	Decision    string  `json:"decision,omitempty"`
}

// ServiceTierError is a routing rejection rendered in the caller's
// language. Callers decide the transport shape.
type ServiceTierError struct {
	StatusCode int
	Code       types.ErrorCode
	MessageID  string
	Params     map[string]any
}

func (e *ServiceTierError) Message(c *gin.Context) string {
	return i18n.T(c, e.MessageID, e.Params)
}

// APIError renders the rejection as a non-retryable relay error.
func (e *ServiceTierError) APIError(c *gin.Context) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(e.Message(c)), e.Code, e.StatusCode, types.ErrOptionWithSkipRetry())
}

// ServiceTierRouteFrom returns the request's route, or nil for requests on
// ordinary group routing.
func ServiceTierRouteFrom(c *gin.Context) *ServiceTierRoute {
	if c == nil {
		return nil
	}
	route, _ := common.GetContextKeyType[*ServiceTierRoute](c, constant.ContextKeyServiceTierRoute)
	return route
}

// serviceTierEndpointError rejects endpoints whose retry and billing
// semantics aggregation does not cover, so a turned-off tier cannot be
// reached through another protocol.
func serviceTierEndpointError(c *gin.Context, modelName string) *ServiceTierError {
	params := map[string]any{"Model": modelName}
	request := c.Request
	if strings.EqualFold(request.Header.Get("Upgrade"), "websocket") || request.URL.Path == "/v1/realtime" {
		return &ServiceTierError{StatusCode: http.StatusBadRequest, Code: types.ErrorCodeInvalidRequest, MessageID: i18n.MsgServiceTierWebSocketUnsupported, Params: params}
	}
	if request.Method == http.MethodPost {
		switch request.URL.Path {
		case "/v1/chat/completions", "/v1/completions", "/v1/messages", "/v1/responses", "/v1/responses/compact", "/pg/chat/completions":
			return nil
		}
		for _, prefix := range []string{"/v1beta/models/", "/v1/models/"} {
			if rest, ok := strings.CutPrefix(request.URL.Path, prefix); ok {
				if _, action, found := strings.Cut(rest, ":"); found && (action == "generateContent" || action == "streamGenerateContent") {
					return nil
				}
			}
		}
	}
	return &ServiceTierError{StatusCode: http.StatusBadRequest, Code: types.ErrorCodeInvalidRequest, MessageID: i18n.MsgServiceTierEndpointUnsupported, Params: params}
}

// ResolveServiceTierRoute returns the route for modelName when an enabled
// aggregation policy applies to the requesting user, or nil when the request
// keeps ordinary group routing. The route is stored on the request.
func ResolveServiceTierRoute(c *gin.Context, modelName string) (*ServiceTierRoute, *ServiceTierError) {
	snapshot, snapshotErr := currentServiceTierSnapshot()
	if snapshotErr != nil {
		return nil, &ServiceTierError{
			StatusCode: http.StatusServiceUnavailable,
			Code:       types.ErrorCodeQueryDataError,
			MessageID:  i18n.MsgServiceTierConfigUnavailable,
		}
	}
	if snapshot == nil || modelName == "" {
		return nil, nil
	}
	family, policy, scope := snapshot.resolve(modelName)
	if family == nil || policy == nil || !policy.Record.Enabled {
		return nil, nil
	}
	userID := c.GetInt("id")
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	if userGroup == "" && userID > 0 {
		if user, err := model.GetUserCache(userID); err == nil {
			userGroup = user.Group
		}
	}
	strategy := policy.Config.Strategy
	if len(strategy.UserGroups) > 0 && !slices.Contains(strategy.UserGroups, userGroup) {
		return nil, nil
	}
	if endpointErr := serviceTierEndpointError(c, modelName); endpointErr != nil {
		return nil, endpointErr
	}

	// Aggregation chooses channels, never models: the model must already be
	// available to one of the groups this request may use.
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	groups := []string{usingGroup}
	if usingGroup == "auto" {
		groups = GetRequestAutoGroups(c, userGroup)
	}
	if !slices.ContainsFunc(groups, func(group string) bool { return model.GroupHasEnabledModel(group, modelName) }) {
		return nil, &ServiceTierError{StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeModelNotFound, MessageID: i18n.MsgServiceTierModelUnavailable, Params: map[string]any{"Model": modelName, "Group": usingGroup}}
	}

	settings, err := GetTokenServiceTierSettings(userID, c.GetInt("token_id"))
	if err != nil {
		common.SysError(common.LogText("failed to load service tier settings (user_id=%d): %s", userID, err.Error()))
		return nil, &ServiceTierError{StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeQueryDataError, MessageID: i18n.MsgServiceTierConfigUnavailable}
	}
	steps, preferred := BuildServiceTierChain(policy.Config, settings)
	if len(steps) == 0 {
		return nil, &ServiceTierError{StatusCode: http.StatusForbidden, Code: types.ErrorCodeAccessDenied, MessageID: i18n.MsgServiceTierNoTier, Params: map[string]any{"Model": modelName}}
	}
	route := &ServiceTierRoute{
		Model:         modelName,
		FamilyID:      family.Id,
		FamilyName:    family.Name,
		PolicyScope:   scope,
		PolicyID:      policy.Record.Id,
		PolicyVersion: policy.Record.Version,
		Preference:    settings.Preference,
		PreferredTier: preferred,
		Steps:         steps,
		strategy:      strategy,
		usingGroup:    usingGroup,
		startedAt:     time.Now(),
		stepAttempts:  make([]int, len(steps)),
		failed:        make(map[int]bool),
	}
	common.SetContextKey(c, constant.ContextKeyServiceTierRoute, route)
	return route, nil
}

// RetryBudget is the number of retries after the first attempt.
func (r *ServiceTierRoute) RetryBudget() int {
	return max(r.strategy.MaxAttempts-1, 0)
}

// CeilingRatio is the highest ratio this request may be charged.
func (r *ServiceTierRoute) CeilingRatio() float64 {
	return serviceTierCeilingRatio(r.Steps)
}

type serviceTierCandidate struct {
	channel  *model.Channel
	priority int64
	weight   int
}

// candidates lists the step's eligible channels. Channels in a local
// cooldown, and channels that already failed in this request when the
// policy does not skip them, are kept only as a last resort.
func (r *ServiceTierRoute) candidates(step ServiceTierStep, filters []dto.ChannelFilter) []serviceTierCandidate {
	preferred := make([]serviceTierCandidate, 0, len(step.channels))
	fallback := make([]serviceTierCandidate, 0)
	now := time.Now()
	for _, entry := range step.channels {
		failed := r.failed[entry.ChannelId]
		if failed && r.strategy.SkipFailedChannels {
			continue
		}
		channel, err := model.CacheGetChannel(entry.ChannelId)
		if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		if len(serviceTierSupportedMembers(channel, []string{r.Model})) == 0 {
			continue
		}
		if ok, _ := model.ChannelSatisfiesFilters(channel, r.Model, filters); !ok {
			continue
		}
		candidate := serviceTierCandidate{channel: channel, priority: entry.Priority, weight: entry.Weight}
		if failed || serviceTierHealth.coolingDown(entry.ChannelId, now) {
			fallback = append(fallback, candidate)
			continue
		}
		preferred = append(preferred, candidate)
	}
	if len(preferred) > 0 {
		return preferred
	}
	return fallback
}

// pickServiceTierCandidate takes the highest priority and chooses within it
// by weight, the same order the administrator page lists.
func pickServiceTierCandidate(candidates []serviceTierCandidate) *model.Channel {
	top := candidates[0].priority
	for _, candidate := range candidates {
		top = max(top, candidate.priority)
	}
	level := make([]serviceTierCandidate, 0, len(candidates))
	total := 0
	for _, candidate := range candidates {
		if candidate.priority == top {
			level = append(level, candidate)
			total += candidate.weight
		}
	}
	if total <= 0 {
		return level[rand.Intn(len(level))].channel
	}
	point := rand.Intn(total)
	for _, candidate := range level {
		point -= candidate.weight
		if point < 0 {
			return candidate.channel
		}
	}
	return level[len(level)-1].channel
}

// nextStep returns the first step from the current one that still has a
// candidate, or -1.
func (r *ServiceTierRoute) nextStep(filters []dto.ChannelFilter) (int, []serviceTierCandidate) {
	for index := r.stepIndex; index < len(r.Steps); index++ {
		if index == r.stepIndex && r.stepAttempts[index] > 0 && !r.strategy.SameTierFailover {
			continue
		}
		if candidates := r.candidates(r.Steps[index], filters); len(candidates) > 0 {
			return index, candidates
		}
	}
	return -1, nil
}

// NextChannel chooses the channel for the next attempt. Same-tier channels
// come first; a later step is used only when the current tier has no
// candidate left, which is what makes it an escalation.
func (r *ServiceTierRoute) NextChannel(c *gin.Context) (*model.Channel, ServiceTierStep, *ServiceTierError) {
	filters := GetChannelConstraints(c).Filters
	index, candidates := r.nextStep(filters)
	if index < 0 {
		tiers := make([]string, 0, len(r.Steps))
		for _, step := range r.Steps {
			tiers = append(tiers, ServiceTierDisplayName(c, step.Tier))
		}
		return nil, ServiceTierStep{}, &ServiceTierError{
			StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeModelNotFound, MessageID: i18n.MsgServiceTierNoChannel,
			Params: map[string]any{"Model": r.Model, "Tiers": strings.Join(tiers, ", ")},
		}
	}
	var channel *model.Channel
	if r.attempts == 0 {
		// Session affinity is a preference inside the starting tier only;
		// it never pulls a request into another tier.
		if preferredID, found := GetPreferredChannelByAffinity(c, r.Model, r.usingGroup); found {
			for _, candidate := range candidates {
				if candidate.channel.Id == preferredID {
					channel = candidate.channel
					MarkChannelAffinityUsed(c, r.usingGroup, preferredID)
					break
				}
			}
		}
	}
	if channel == nil {
		channel = pickServiceTierCandidate(candidates)
	}
	step := r.Steps[index]
	r.stepIndex = index
	r.stepAttempts[index]++
	r.attempts++
	r.trail = append(r.trail, ServiceTierAttempt{
		Attempt: r.attempts, Tier: step.Tier, Ratio: step.Ratio, Escalation: step.Escalation,
		ChannelID: channel.Id, ChannelName: channel.Name, Outcome: "pending",
	})
	return channel, step, nil
}

// SelectFirstChannel picks the first attempt's channel for the HTTP
// distributor and prices the request at that tier.
func (r *ServiceTierRoute) SelectFirstChannel(c *gin.Context) (*model.Channel, *ServiceTierError) {
	channel, step, err := r.NextChannel(c)
	if err != nil {
		return nil, err
	}
	common.SetContextKey(c, constant.ContextKeyServiceTierRatio, step.Ratio)
	common.SetContextKey(c, constant.ContextKeyServiceTier, step.Tier)
	return channel, nil
}

// classifyServiceTierError maps a failed attempt to the categories an
// administrator can allow to fail over.
func classifyServiceTierError(err *types.NewAPIError) string {
	switch {
	case types.IsChannelError(err):
		return ServiceTierErrorChannel
	case types.IsSkipRetryError(err):
		return ServiceTierErrorLocal
	case err.GetErrorCode() == types.ErrorCodeDoRequestFailed:
		return ServiceTierErrorTimeout
	}
	code := err.StatusCode
	switch {
	case code == http.StatusTooManyRequests:
		return ServiceTierErrorRateLimited
	case code == http.StatusRequestTimeout || code == http.StatusGatewayTimeout || code == 524:
		return ServiceTierErrorTimeout
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return ServiceTierErrorChannel
	case code >= 500 && code <= 599:
		return ServiceTierErrorServer
	case code >= 400 && code < 500:
		return ServiceTierErrorClient
	case code >= 200 && code < 300:
		return ServiceTierErrorInvalidResponse
	}
	return ServiceTierErrorServer
}

// DecideRetry records the failed attempt and decides whether the request
// moves to another channel. Unlike the global retry rule it also stops once
// the response has started, the client has gone, the policy's attempt or
// time budget is spent, or no allowed channel is left.
func (r *ServiceTierRoute) DecideRetry(c *gin.Context, channelID int, err *types.NewAPIError) PolicyDecision {
	stop := PolicyDecision{Action: "stop", Source: "service_tier"}
	if err == nil {
		stop.Reason = "request_completed"
		return stop
	}
	category := classifyServiceTierError(err)
	r.failed[channelID] = true
	if slices.Contains(ServiceTierRelayableErrors, category) {
		serviceTierHealth.recordFailure(channelID, category, err, r.strategy, time.Now())
	}
	decision := r.decide(c, err, category)
	if last := len(r.trail) - 1; last >= 0 && r.trail[last].ChannelID == channelID {
		entry := &r.trail[last]
		entry.Outcome, entry.Status, entry.ErrorCode, entry.Category = "failure", err.StatusCode, string(err.GetErrorCode()), category
		entry.Decision = decision.Reason
	}
	return decision
}

func (r *ServiceTierRoute) decide(c *gin.Context, err *types.NewAPIError, category string) PolicyDecision {
	stop := PolicyDecision{Action: "stop", Source: "service_tier"}
	switch {
	case c.Writer.Written():
		// Output already reached the client; another provider cannot
		// continue it transparently.
		stop.Reason = "response_started"
	case c.Request.Context().Err() != nil:
		stop.Reason = "client_cancelled"
	case ShouldSkipRetryAfterChannelAffinityFailure(c):
		stop.Reason = "strict_session"
	case GetChannelConstraints(c).SuppressesRetry():
		stop.Reason = "pinned_channel"
	case !slices.Contains(ServiceTierRelayableErrors, category):
		stop.Reason = "non_retryable_error"
	case operation_setting.IsAlwaysSkipRetryCode(err.GetErrorCode()) || operation_setting.IsAlwaysSkipRetryStatusCode(err.StatusCode):
		stop.Reason = "system_retry_exclusion"
	case !slices.Contains(r.strategy.RetryOn, category):
		stop.Reason = "error_not_relayable"
	case r.attempts >= r.strategy.MaxAttempts:
		stop.Reason = "attempt_budget_exhausted"
	case r.strategy.MaxTotalSeconds > 0 && time.Since(r.startedAt) >= time.Duration(r.strategy.MaxTotalSeconds)*time.Second:
		stop.Reason = "time_budget_exhausted"
	default:
		index, _ := r.nextStep(GetChannelConstraints(c).Filters)
		switch {
		case index < 0:
			stop.Reason = "no_remaining_channel"
		case index == r.stepIndex:
			return PolicyDecision{Action: "retry", Reason: "same_tier_failover", Source: "service_tier"}
		default:
			return PolicyDecision{Action: "retry", Reason: "tier_escalation", Source: "service_tier"}
		}
	}
	return stop
}

// MarkSuccess records that the current attempt served the request.
func (r *ServiceTierRoute) MarkSuccess(channelID int) {
	serviceTierHealth.recordSuccess(channelID)
	if last := len(r.trail) - 1; last >= 0 && r.trail[last].ChannelID == channelID {
		r.trail[last].Outcome = "success"
	}
}

// currentStep is the tier of the latest attempt.
func (r *ServiceTierRoute) currentStep() ServiceTierStep {
	return r.Steps[r.stepIndex]
}

// ReserveServiceTierRatio prices the coming attempt at its tier. Moving to a
// pricier tier first reserves the higher estimate from funds available now;
// if that fails the request stops before the tier is called.
func ReserveServiceTierRatio(c *gin.Context, info *relaycommon.RelayInfo, route *ServiceTierRoute, step ServiceTierStep) *types.NewAPIError {
	if step.Ratio > route.CeilingRatio() {
		return types.NewErrorWithStatusCode(errors.New("service tier ratio exceeds the request ceiling"), types.ErrorCodeModelPriceError, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	currentRatio := info.PriceData.GroupRatioInfo.GroupRatio
	if step.Ratio > currentRatio && currentRatio > 0 && !info.PriceData.FreeModel {
		var target int
		var err error
		if snapshot := info.TieredBillingSnapshot; snapshot != nil && snapshot.BillingMode == "tiered_expr" {
			target, err = billingexpr.QuotaRoundStrict(snapshot.EstimatedQuotaBeforeGroup * step.Ratio)
		} else {
			target, err = common.QuotaFromFloatStrict(info.ServiceTierPreConsumeQuotaBeforeGroup * step.Ratio)
		}
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if target > 0 {
			var reserveErr error
			if info.Billing == nil {
				if apiErr := PreConsumeBilling(c, target, info); apiErr != nil {
					reserveErr = apiErr
				}
			} else if session, ok := info.Billing.(*BillingSession); ok {
				reserveErr = session.ReserveStrict(target)
			} else {
				reserveErr = errors.New("billing session cannot reserve strictly")
			}
			if reserveErr != nil {
				logger.LogWarn(c, common.LogText("service tier escalation stopped: tier=%s target_quota=%d error=%s", step.Tier, target, reserveErr.Error()))
				if last := len(route.trail) - 1; last >= 0 {
					route.trail[last].Outcome, route.trail[last].Decision = "blocked", "insufficient_quota"
				}
				return types.NewErrorWithStatusCode(
					errors.New(i18n.T(c, i18n.MsgServiceTierEscalationQuota, map[string]any{"Model": route.Model, "Tier": ServiceTierDisplayName(c, step.Tier)})),
					types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
					types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog(),
				)
			}
			info.FinalPreConsumedQuota = info.Billing.GetPreConsumedQuota()
		}
		info.PriceData.QuotaToPreConsume = target
	}
	common.SetContextKey(c, constant.ContextKeyServiceTierRatio, step.Ratio)
	common.SetContextKey(c, constant.ContextKeyServiceTier, step.Tier)
	return nil
}

// ServiceTierDisplayName is the tier's name in the request's language.
func ServiceTierDisplayName(c *gin.Context, tier string) string {
	switch tier {
	case model.ServiceTierEconomy:
		return i18n.T(c, i18n.MsgServiceTierNameEconomy)
	case model.ServiceTierBalanced:
		return i18n.T(c, i18n.MsgServiceTierNameBalanced)
	case model.ServiceTierPerformance:
		return i18n.T(c, i18n.MsgServiceTierNamePerformance)
	}
	return tier
}

// AppendServiceTierLogInfo records the tier outcome on a usage or error log.
// The public part holds only tiers and ratios; channels stay in admin_info.
func AppendServiceTierLogInfo(c *gin.Context, other *model.LogOther) {
	route := ServiceTierRouteFrom(c)
	if route == nil || other == nil || route.attempts == 0 {
		return
	}
	final := route.currentStep()
	other.SetPublic("service_tier", map[string]any{
		"family":         route.FamilyName,
		"preference":     route.Preference,
		"preferred_tier": route.PreferredTier,
		"final_tier":     final.Tier,
		"escalated":      final.Tier != route.PreferredTier,
		"attempts":       route.attempts,
		"ratio":          final.Ratio,
	})
	chain := make([]map[string]any, 0, len(route.Steps))
	for _, step := range route.Steps {
		chain = append(chain, map[string]any{"tier": step.Tier, "ratio": step.Ratio, "escalation": step.Escalation})
	}
	other.SetAdmin("service_tier_route", map[string]any{
		"family_id":      route.FamilyID,
		"policy_id":      route.PolicyID,
		"policy_version": route.PolicyVersion,
		"policy_scope":   route.PolicyScope,
		"ceiling_ratio":  route.CeilingRatio(),
		"chain":          chain,
		"attempts":       slices.Clone(route.trail),
	})
}

// ---------------------------------------------------------------------------
// Channel health (per node)
// ---------------------------------------------------------------------------

const serviceTierHealthWindow = 15 * time.Minute

type serviceTierChannelHealth struct {
	consecutiveFailures int
	cooldownUntil       time.Time
	failures            []time.Time
	lastFailureAt       time.Time
	lastStatus          int
	lastCategory        string
	lastErrorCode       string
	lastSuccessAt       time.Time
}

type serviceTierHealthTracker struct {
	mu       sync.Mutex
	channels map[int]*serviceTierChannelHealth
}

var serviceTierHealth = &serviceTierHealthTracker{channels: make(map[int]*serviceTierChannelHealth)}

func (t *serviceTierHealthTracker) recordFailure(channelID int, category string, err *types.NewAPIError, strategy model.AggregationStrategy, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	health := t.channels[channelID]
	if health == nil {
		health = &serviceTierChannelHealth{}
		t.channels[channelID] = health
	}
	health.consecutiveFailures++
	health.lastFailureAt, health.lastStatus, health.lastCategory, health.lastErrorCode = now, err.StatusCode, category, string(err.GetErrorCode())
	health.failures = append(slices.DeleteFunc(health.failures, func(at time.Time) bool { return now.Sub(at) > serviceTierHealthWindow }), now)
	if len(health.failures) > 100 {
		health.failures = health.failures[len(health.failures)-100:]
	}
	if strategy.CooldownFailures > 0 && health.consecutiveFailures >= strategy.CooldownFailures {
		health.cooldownUntil = now.Add(time.Duration(strategy.CooldownSeconds) * time.Second)
		health.consecutiveFailures = 0
	}
}

func (t *serviceTierHealthTracker) recordSuccess(channelID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	health := t.channels[channelID]
	if health == nil {
		health = &serviceTierChannelHealth{}
		t.channels[channelID] = health
	}
	health.consecutiveFailures = 0
	health.cooldownUntil = time.Time{}
	health.lastSuccessAt = time.Now()
}

func (t *serviceTierHealthTracker) coolingDown(channelID int, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	health := t.channels[channelID]
	return health != nil && now.Before(health.cooldownUntil)
}

// ServiceTierChannelHealth is this node's recent aggregation outcome for a
// channel, shown on the administrator page.
type ServiceTierChannelHealth struct {
	RecentFailures int    `json:"recent_failures"`
	LastFailureAt  int64  `json:"last_failure_at,omitempty"`
	LastStatus     int    `json:"last_status,omitempty"`
	LastCategory   string `json:"last_category,omitempty"`
	LastErrorCode  string `json:"last_error_code,omitempty"`
	LastSuccessAt  int64  `json:"last_success_at,omitempty"`
	CooldownUntil  int64  `json:"cooldown_until,omitempty"`
}

func (t *serviceTierHealthTracker) view(channelID int, now time.Time) ServiceTierChannelHealth {
	t.mu.Lock()
	defer t.mu.Unlock()
	health := t.channels[channelID]
	if health == nil {
		return ServiceTierChannelHealth{}
	}
	result := ServiceTierChannelHealth{LastStatus: health.lastStatus, LastCategory: health.lastCategory, LastErrorCode: health.lastErrorCode}
	for _, at := range health.failures {
		if now.Sub(at) <= serviceTierHealthWindow {
			result.RecentFailures++
		}
	}
	if !health.lastFailureAt.IsZero() {
		result.LastFailureAt = health.lastFailureAt.Unix()
	}
	if !health.lastSuccessAt.IsZero() {
		result.LastSuccessAt = health.lastSuccessAt.Unix()
	}
	if now.Before(health.cooldownUntil) {
		result.CooldownUntil = health.cooldownUntil.Unix()
	}
	return result
}
