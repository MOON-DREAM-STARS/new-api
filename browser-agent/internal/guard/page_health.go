package guard

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	pageStateUnknown  = ""
	pageStateReady    = "READY"
	pageStateRetrying = "RETRYING"
	pageStateFailed   = "FAILED"
)

var pageRetryDelays = []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second}
var pageContentProbeDelays = []time.Duration{2 * time.Second, 2 * time.Second}

const (
	pageErrorBlankPage         = "ERR_BLANK_PAGE"
	pageErrorUnresponsive      = "ERR_PAGE_UNRESPONSIVE"
	defaultContentProbeTimeout = 3 * time.Second
	pageContentReadyExpression = `(() => {
		const body = document.body;
		return document.readyState === 'complete' &&
			!!body &&
			body.childElementCount > 0 &&
			(body.innerText || '').trim().length > 0;
	})()`
)

type pageHealthPhase uint8

const (
	pagePhaseUnknown pageHealthPhase = iota
	pagePhaseReady
	pagePhaseWaiting
	pagePhaseNavigating
	pagePhaseFailed
)

type pageHealthSnapshot struct {
	state     string
	pageError string
	attempts  int
	updatedAt int64
}

// pageHealthController observes the main page document and owns the bounded
// automatic retry schedule. It writes status through navigationController so
// every navigation.json update remains one atomic write.
type pageHealthController struct {
	client              *cdpClient
	navigation          *navigationController
	logger              *slog.Logger
	startURL            string
	retryDelays         []time.Duration
	contentProbeDelays  []time.Duration
	contentProbeTimeout time.Duration

	mu               sync.Mutex
	state            string
	pageError        string
	attempts         int
	updatedAt        int64
	errorPage        bool
	phase            pageHealthPhase
	retryAt          time.Time
	mainSessionID    string
	mainFrameID      string
	probeGeneration  uint64
	documentRequests map[string]string

	wake chan struct{}
}

func newPageHealthController(client *cdpClient, navigation *navigationController, startURL string, retryDelays []time.Duration, contentProbeDelays []time.Duration, contentProbeTimeout time.Duration, logger *slog.Logger) *pageHealthController {
	if logger == nil {
		logger = slog.Default()
	}
	if retryDelays == nil {
		retryDelays = pageRetryDelays
	}
	if len(contentProbeDelays) == 0 {
		contentProbeDelays = pageContentProbeDelays
	}
	if contentProbeTimeout <= 0 {
		contentProbeTimeout = defaultContentProbeTimeout
	}
	return &pageHealthController{
		client:              client,
		navigation:          navigation,
		logger:              logger,
		startURL:            strings.TrimSpace(startURL),
		retryDelays:         append([]time.Duration(nil), retryDelays...),
		contentProbeDelays:  append([]time.Duration(nil), contentProbeDelays...),
		contentProbeTimeout: contentProbeTimeout,
		state:               pageStateUnknown,
		documentRequests:    make(map[string]string),
		wake:                make(chan struct{}, 1),
	}
}

func validPageState(state string) bool {
	switch state {
	case pageStateReady, pageStateRetrying, pageStateFailed:
		return true
	default:
		return false
	}
}

func (p *pageHealthController) run(ctx context.Context) {
	if p == nil {
		return
	}
	for {
		delay, ok := p.nextRetryDelay()
		var timer *time.Timer
		var timerC <-chan time.Time
		if ok {
			if delay < 0 {
				delay = 0
			}
			timer = time.NewTimer(delay)
			timerC = timer.C
		}

		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-p.wake:
			if timer != nil {
				timer.Stop()
			}
		case <-timerC:
			p.fireRetry(ctx)
		}
	}
}

func (p *pageHealthController) nextRetryDelay() (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phase != pagePhaseWaiting || p.retryAt.IsZero() {
		return 0, false
	}
	return time.Until(p.retryAt), true
}

func (p *pageHealthController) notify() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *pageHealthController) observeFrameNavigated(sessionID, parentID, frameID, rawURL string) {
	if !p.isMainSession(sessionID) {
		return
	}
	if parentID != "" {
		return
	}
	p.mu.Lock()
	p.mainSessionID = sessionID
	if frameID != "" {
		p.mainFrameID = frameID
	}
	p.probeGeneration++
	p.mu.Unlock()

	if isChromeErrorPage(rawURL) {
		p.recordFailure(sessionID, "")
	}
}

func (p *pageHealthController) observeRequestWillBeSent(sessionID string, params json.RawMessage) {
	if !p.isMainSession(sessionID) {
		return
	}
	var event struct {
		RequestID string `json:"requestId"`
		Type      string `json:"type"`
		FrameID   string `json:"frameId"`
	}
	if err := json.Unmarshal(params, &event); err != nil || event.Type != "Document" || event.RequestID == "" {
		return
	}
	p.mu.Lock()
	if p.mainSessionID == "" {
		p.mainSessionID = sessionID
	}
	if p.mainFrameID == "" {
		// The first document request in the app page session belongs to the
		// main frame. Subsequent request events refine this from frame events.
		p.mainFrameID = event.FrameID
	}
	p.documentRequests[event.RequestID] = event.FrameID
	p.mu.Unlock()
}

func (p *pageHealthController) observeLoadingFailed(sessionID string, params json.RawMessage) {
	if !p.isMainSession(sessionID) {
		return
	}
	var event struct {
		RequestID string `json:"requestId"`
		Type      string `json:"type"`
		ErrorText string `json:"errorText"`
	}
	if err := json.Unmarshal(params, &event); err != nil || event.Type != "Document" {
		return
	}

	p.mu.Lock()
	frameID, known := p.documentRequests[event.RequestID]
	delete(p.documentRequests, event.RequestID)
	mainDocument := !known || p.mainFrameID == "" || frameID == p.mainFrameID
	p.mu.Unlock()
	if mainDocument {
		// Chrome reports ERR_ABORTED when a document navigation is superseded
		// by another navigation (for example, an in-app redirect). The old
		// request being canceled is not a page failure: the replacement
		// navigation or the current document owns the page now.
		if normalizePageError(event.ErrorText) == "ERR_ABORTED" {
			return
		}
		p.recordFailure(sessionID, event.ErrorText)
	}
}

func (p *pageHealthController) observeLoadingFinished(sessionID string, params json.RawMessage) {
	if !p.isMainSession(sessionID) {
		return
	}
	var event struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(params, &event); err != nil || event.RequestID == "" {
		return
	}
	p.mu.Lock()
	delete(p.documentRequests, event.RequestID)
	p.mu.Unlock()
}

func (p *pageHealthController) observeLoadEvent(ctx context.Context, sessionID string) {
	p.probeCurrentPage(ctx, sessionID)
}

// probeCurrentPage checks the already-attached main page without waiting for
// another Page.loadEventFired. This covers a target whose initial navigation
// was superseded before the Page domain was enabled, which would otherwise
// leave Chromium at about:blank with no event to trigger recovery.
func (p *pageHealthController) probeCurrentPage(ctx context.Context, sessionID string) {
	if !p.isMainSession(sessionID) {
		return
	}

	p.mu.Lock()
	p.mainSessionID = sessionID
	p.probeGeneration++
	generation := p.probeGeneration
	p.mu.Unlock()

	go p.probeContent(ctx, sessionID, generation)
}

func (p *pageHealthController) probeContent(ctx context.Context, sessionID string, generation uint64) {
	var probeErr error
	for _, delay := range p.contentProbeDelays {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if !p.probeIsCurrent(generation) {
			return
		}

		ready, err := p.evaluateContentReady(ctx, sessionID)
		if err != nil {
			probeErr = err
		}
		if !p.probeIsCurrent(generation) {
			return
		}
		if ready {
			p.recordSuccess(sessionID, generation)
			return
		}
	}

	errorCode := pageErrorBlankPage
	if probeErr != nil {
		errorCode = pageErrorUnresponsive
	}
	p.recordFailureForGeneration(sessionID, errorCode, generation)
}

func (p *pageHealthController) probeIsCurrent(generation uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.probeGeneration == generation
}

func (p *pageHealthController) evaluateContentReady(ctx context.Context, sessionID string) (bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, p.contentProbeTimeout)
	defer cancel()
	raw, err := p.client.callResult(probeCtx, sessionID, "Runtime.evaluate", map[string]any{
		"expression":    pageContentReadyExpression,
		"returnByValue": true,
	})
	if err != nil {
		return false, err
	}
	var response struct {
		Result struct {
			Value bool `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return false, err
	}
	return response.Result.Value, nil
}

func (p *pageHealthController) recordFailure(sessionID, rawError string) {
	p.recordFailureForGeneration(sessionID, rawError, 0)
}

func (p *pageHealthController) recordFailureForGeneration(sessionID, rawError string, generation uint64) {
	errorCode := normalizePageError(rawError)
	p.mu.Lock()
	if generation != 0 && p.probeGeneration != generation {
		p.mu.Unlock()
		return
	}
	p.probeGeneration++
	if sessionID != "" {
		p.mainSessionID = sessionID
	}
	if errorCode != "" {
		p.pageError = errorCode
	}
	p.errorPage = true

	switch p.phase {
	case pagePhaseReady, pagePhaseUnknown:
		p.attempts = 0
		if p.startURL == "" || len(p.retryDelays) == 0 {
			p.phase = pagePhaseFailed
			p.state = pageStateFailed
			p.retryAt = time.Time{}
		} else {
			p.phase = pagePhaseWaiting
			p.state = pageStateRetrying
			p.retryAt = time.Now().Add(p.retryDelays[0])
		}
	case pagePhaseWaiting:
		p.state = pageStateRetrying
	case pagePhaseNavigating:
		if p.attempts >= len(p.retryDelays) {
			p.phase = pagePhaseFailed
			p.state = pageStateFailed
			p.retryAt = time.Time{}
		} else {
			p.phase = pagePhaseWaiting
			p.state = pageStateRetrying
			p.retryAt = time.Now().Add(p.retryDelays[p.attempts])
		}
	case pagePhaseFailed:
		p.state = pageStateFailed
		p.retryAt = time.Time{}
	}
	p.updatedAt = time.Now().Unix()
	status := p.statusLocked()
	p.mu.Unlock()
	p.publish(status)
	p.notify()
}

func (p *pageHealthController) recordSuccess(sessionID string, generation uint64) {
	p.mu.Lock()
	if p.probeGeneration != generation {
		p.mu.Unlock()
		return
	}
	if sessionID != "" {
		p.mainSessionID = sessionID
	}
	p.state = pageStateReady
	p.pageError = ""
	p.attempts = 0
	p.errorPage = false
	p.phase = pagePhaseReady
	p.retryAt = time.Time{}
	p.updatedAt = time.Now().Unix()
	status := p.statusLocked()
	p.mu.Unlock()
	p.publish(status)
	p.notify()
}

func (p *pageHealthController) fireRetry(ctx context.Context) {
	p.mu.Lock()
	if p.phase != pagePhaseWaiting {
		p.mu.Unlock()
		return
	}
	if p.startURL == "" || p.attempts >= len(p.retryDelays) {
		p.phase = pagePhaseFailed
		p.state = pageStateFailed
		p.retryAt = time.Time{}
		p.updatedAt = time.Now().Unix()
		status := p.statusLocked()
		p.mu.Unlock()
		p.publish(status)
		return
	}
	p.phase = pagePhaseNavigating
	p.attempts++
	p.state = pageStateRetrying
	p.retryAt = time.Time{}
	p.updatedAt = time.Now().Unix()
	sessionID := p.mainSessionID
	startURL := p.startURL
	status := p.statusLocked()
	p.mu.Unlock()

	p.publish(status)
	if sessionID == "" {
		p.recordFailure("", "")
		return
	}
	if err := p.client.call(ctx, sessionID, "Page.navigate", map[string]any{"url": startURL}); err != nil {
		p.logger.Debug("page retry navigation failed",
			"event", "page_retry_navigation_failed",
			"component", "guard",
			"error", err,
		)
		p.recordFailure(sessionID, "")
	}
}

func (p *pageHealthController) reload(ctx context.Context, sessionID string) (bool, error) {
	p.mu.Lock()
	if !p.errorPage && p.phase != pagePhaseWaiting && p.phase != pagePhaseFailed {
		p.mu.Unlock()
		return false, nil
	}
	if p.startURL == "" {
		p.mu.Unlock()
		return true, &pageStartURLUnavailableError{}
	}
	p.phase = pagePhaseNavigating
	p.state = pageStateRetrying
	p.attempts = 0
	p.errorPage = true
	p.retryAt = time.Time{}
	p.updatedAt = time.Now().Unix()
	startURL := p.startURL
	status := p.statusLocked()
	p.mu.Unlock()

	p.publish(status)
	p.notify()
	if err := p.client.call(ctx, sessionID, "Page.navigate", map[string]any{"url": startURL}); err != nil {
		p.recordFailure(sessionID, "")
		return true, err
	}
	return true, nil
}

// mainFrameIDForSession reports the main frame id after the initial document
// navigation has been observed. Unknown state is never treated as a main frame.
func (p *pageHealthController) mainFrameIDForSession(sessionID string) (string, bool) {
	if p == nil || sessionID == "" {
		return "", false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mainSessionID != sessionID || p.mainFrameID == "" {
		return "", false
	}
	return p.mainFrameID, true
}

// snapshot is the latest published page state. Callers that only observe the
// page (the creation controller) must not touch the retry schedule.
func (p *pageHealthController) snapshot() pageHealthSnapshot {
	if p == nil {
		return pageHealthSnapshot{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statusLocked()
}

func (p *pageHealthController) statusLocked() pageHealthSnapshot {
	return pageHealthSnapshot{
		state:     p.state,
		pageError: p.pageError,
		attempts:  p.attempts,
		updatedAt: p.updatedAt,
	}
}

func (p *pageHealthController) publish(status pageHealthSnapshot) {
	if p.navigation == nil {
		return
	}
	if err := p.navigation.setPageStatus(status.state, status.pageError, status.attempts, status.updatedAt); err != nil {
		p.logger.Debug("page status write failed",
			"event", "page_status_write_failed",
			"component", "guard",
			"error", err,
		)
	}
}

func (p *pageHealthController) isMainSession(sessionID string) bool {
	if p == nil || sessionID == "" || p.navigation == nil {
		return false
	}
	return p.navigation.currentSession() == sessionID
}

type pageStartURLUnavailableError struct{}

func (*pageStartURLUnavailableError) Error() string {
	return "page start URL unavailable"
}

func normalizePageError(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "net::")
	if len(value) <= len("ERR_") || !strings.HasPrefix(value, "ERR_") {
		return ""
	}
	for _, char := range value {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return ""
		}
	}
	return value
}

func isChromeErrorPage(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "chrome-error")
}
