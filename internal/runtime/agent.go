package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"codexflow/internal/codex"
	"codexflow/internal/config"
	"codexflow/internal/store"
	"codexflow/internal/worklog"
)

type codexClient interface {
	Start(context.Context) error
	Call(context.Context, string, any, any) error
	Reply(context.Context, json.RawMessage, any) error
	Notifications() <-chan codex.Notification
	ServerRequests() <-chan codex.ServerRequest
	StderrLines() <-chan string
}

type sessionDetailCall struct {
	done   chan struct{}
	detail SessionDetail
	err    error
}

type sessionDetailMode int

const (
	sessionDetailModeLimited sessionDetailMode = iota
	sessionDetailModeFull
)

var dashboardThreadSourceKinds = []string{
	"cli",
	"vscode",
	"exec",
	"appServer",
	"unknown",
}

type Agent struct {
	cfg     config.Config
	logger  *slog.Logger
	client  codexClient
	store   *store.Store
	broker  *Broker
	files   ArtifactResolver
	worklog *worklog.Logger
	started time.Time

	sessionDetailMu       sync.Mutex
	sessionDetailInFlight map[string]*sessionDetailCall
}

func NewAgent(cfg config.Config, logger *slog.Logger) *Agent {
	localState, err := store.OpenLocalStateDB(cfg.StateDBPath)
	if err != nil {
		logger.Warn("failed to open local state db", "path", cfg.StateDBPath, "error", err)
	}

	sessionStore, err := store.New(localState)
	if err != nil {
		logger.Warn("failed to load persisted local state", "path", cfg.StateDBPath, "error", err)
		sessionStore, _ = store.New(nil)
	}

	return &Agent{
		cfg:     cfg,
		logger:  logger,
		client:  codex.NewClient(cfg.CodexPath, logger),
		store:   sessionStore,
		broker:  NewBroker(),
		worklog: worklog.NewDefault(),
		started: time.Now(),
	}
}

func (a *Agent) SetArtifactResolver(resolver ArtifactResolver) {
	a.files = resolver
}

func (a *Agent) Start(ctx context.Context) error {
	if err := a.client.Start(ctx); err != nil {
		return err
	}

	a.restoreManagedSessions(ctx)

	if err := a.Refresh(ctx); err != nil {
		a.logger.Warn("initial refresh failed", "error", err)
	}

	go a.consumeNotifications(ctx)
	go a.consumeServerRequests(ctx)
	go a.consumeStderr()
	go a.refreshLoop(ctx)

	return nil
}

func (a *Agent) restoreManagedSessions(ctx context.Context) {
	for _, threadID := range a.store.ManagedSessionIDs() {
		resumeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, err := a.ResumeSession(resumeCtx, threadID)
		cancel()
		if err != nil {
			a.logger.Warn("failed to restore managed session", "threadId", threadID, "error", err)
		}
	}
}

func (a *Agent) Subscribe() chan Event {
	return a.broker.Subscribe()
}

func (a *Agent) Unsubscribe(ch chan Event) {
	a.broker.Unsubscribe(ch)
}

func (a *Agent) Dashboard() Dashboard {
	summaries := a.ListSessions()
	approvals := a.PendingRequests()

	stats := DashboardStats{
		TotalSessions:    len(summaries),
		PendingApprovals: len(approvals),
	}
	for _, session := range summaries {
		if session.Loaded {
			stats.LoadedSessions++
		}
		if session.Status == "active" && !session.Ended {
			stats.ActiveSessions++
		}
	}

	return Dashboard{
		Agent: AgentSnapshot{
			Connected:       true,
			StartedAt:       a.started,
			ListenAddr:      a.cfg.ListenAddr,
			CodexBinaryPath: a.cfg.CodexPath,
		},
		Stats:     stats,
		Sessions:  summaries,
		Approvals: approvals,
	}
}

func (a *Agent) ListSessions() []SessionSummary {
	records := a.store.SnapshotSessions()
	pending := a.store.SnapshotPending()
	perThreadPending := make(map[string]int)
	for _, approval := range pending {
		perThreadPending[approval.ThreadID]++
	}

	summaries := make([]SessionSummary, 0, len(records))
	for _, record := range records {
		summaries = append(summaries, toSessionSummary(record, perThreadPending[record.Thread.ID]))
	}
	return summaries
}

func (a *Agent) SessionDetail(ctx context.Context, threadID string) (SessionDetail, error) {
	return a.sessionDetail(ctx, threadID, sessionDetailModeLimited)
}

func (a *Agent) FullSessionDetail(ctx context.Context, threadID string) (SessionDetail, error) {
	return a.sessionDetail(ctx, threadID, sessionDetailModeFull)
}

func (a *Agent) sessionDetail(ctx context.Context, threadID string, mode sessionDetailMode) (SessionDetail, error) {
	callKey := sessionDetailCallKey(threadID, mode)
	call, owner := a.beginSessionDetailCall(callKey)
	if !owner {
		select {
		case <-ctx.Done():
			return SessionDetail{}, ctx.Err()
		case <-call.done:
			return call.detail, call.err
		}
	}

	detail, err := a.loadSessionDetail(ctx, threadID, mode)
	a.finishSessionDetailCall(callKey, call, detail, err)
	return detail, err
}

func sessionDetailCallKey(threadID string, mode sessionDetailMode) string {
	return fmt.Sprintf("%d:%s", mode, threadID)
}

func (a *Agent) beginSessionDetailCall(threadID string) (*sessionDetailCall, bool) {
	a.sessionDetailMu.Lock()
	defer a.sessionDetailMu.Unlock()

	if a.sessionDetailInFlight == nil {
		a.sessionDetailInFlight = make(map[string]*sessionDetailCall)
	}
	if call, ok := a.sessionDetailInFlight[threadID]; ok {
		return call, false
	}

	call := &sessionDetailCall{done: make(chan struct{})}
	a.sessionDetailInFlight[threadID] = call
	return call, true
}

func (a *Agent) finishSessionDetailCall(threadID string, call *sessionDetailCall, detail SessionDetail, err error) {
	a.sessionDetailMu.Lock()
	if current := a.sessionDetailInFlight[threadID]; current == call {
		delete(a.sessionDetailInFlight, threadID)
	}
	call.detail = detail
	call.err = err
	close(call.done)
	a.sessionDetailMu.Unlock()
}

func (a *Agent) loadSessionDetail(ctx context.Context, threadID string, mode sessionDetailMode) (SessionDetail, error) {
	var response codex.ThreadReadResponse
	if err := a.client.Call(ctx, "thread/read", map[string]any{
		"threadId":     threadID,
		"includeTurns": true,
	}, &response); err != nil {
		if strings.Contains(err.Error(), "includeTurns is unavailable before first user message") {
			record, ok := a.store.SnapshotSession(threadID)
			if !ok {
				return SessionDetail{}, err
			}
			return a.renderSessionDetail(record, pendingCountForThread(a.store.SnapshotPending(), threadID), mode), nil
		}
		return SessionDetail{}, err
	}

	a.store.UpsertThread(response.Thread)
	record, ok := a.store.SnapshotSession(threadID)
	if !ok {
		return SessionDetail{}, errors.New("session not found after refresh")
	}

	pendingCount := pendingCountForThread(a.store.SnapshotPending(), threadID)

	return a.renderSessionDetail(record, pendingCount, mode), nil
}

func (a *Agent) renderSessionDetail(record store.SessionRecord, pendingCount int, mode sessionDetailMode) SessionDetail {
	if mode == sessionDetailModeFull {
		return toFullSessionDetail(record, pendingCount, a.files)
	}
	return toSessionDetail(record, pendingCount, a.files)
}

func (a *Agent) PendingRequests() []PendingRequestView {
	pending := a.store.SnapshotPending()
	views := make([]PendingRequestView, 0, len(pending))
	for _, request := range pending {
		views = append(views, PendingRequestView{
			ID:        request.ID,
			Method:    request.Method,
			Kind:      requestKind(request.Method),
			ThreadID:  request.ThreadID,
			TurnID:    request.TurnID,
			ItemID:    request.ItemID,
			Reason:    request.Reason,
			Summary:   request.Summary,
			Choices:   cloneStrings(request.Choices),
			CreatedAt: request.CreatedAt,
			Params:    request.Params,
		})
	}
	return views
}

func (a *Agent) ResolveRequest(ctx context.Context, requestID string, result json.RawMessage) error {
	request, ok := a.store.DeletePending(requestID)
	if !ok {
		return fmt.Errorf("pending request %s not found", requestID)
	}

	var payload any
	if len(result) > 0 {
		if err := json.Unmarshal(result, &payload); err != nil {
			return fmt.Errorf("decode resolve payload: %w", err)
		}
	}

	if err := a.client.Reply(ctx, request.RawRPCRequestID, payload); err != nil {
		return err
	}

	a.broker.Publish("approval.resolved", PendingRequestView{
		ID:        request.ID,
		Method:    request.Method,
		Kind:      requestKind(request.Method),
		ThreadID:  request.ThreadID,
		TurnID:    request.TurnID,
		ItemID:    request.ItemID,
		Reason:    request.Reason,
		Summary:   request.Summary,
		Choices:   cloneStrings(request.Choices),
		CreatedAt: request.CreatedAt,
		Params:    request.Params,
	})
	return nil
}

func (a *Agent) Refresh(ctx context.Context) error {
	threads, err := a.fetchThreads(ctx)
	if err != nil {
		return err
	}

	loadedIDs, err := a.fetchLoadedThreadIDs(ctx)
	if err != nil {
		return err
	}

	loaded := make(map[string]bool, len(loadedIDs))
	for _, id := range loadedIDs {
		loaded[id] = true
	}

	a.store.ReplaceSessions(threads, loaded)
	a.broker.Publish("sessions.refreshed", a.ListSessions())
	return nil
}

func (a *Agent) StartSession(ctx context.Context, cwd, prompt string) (SessionSummary, error) {
	var threadResp codex.ThreadStartResponse
	if err := a.client.Call(ctx, "thread/start", map[string]any{
		"cwd":                    emptyToNil(cwd),
		"experimentalRawEvents":  true,
		"persistExtendedHistory": true,
	}, &threadResp); err != nil {
		return SessionSummary{}, err
	}

	a.store.UpsertThread(threadResp.Thread)
	a.store.SetSessionEnded(threadResp.Thread.ID, false)
	a.store.SetSessionManaged(threadResp.Thread.ID, true)
	a.store.SetSessionLoaded(threadResp.Thread.ID, true)

	if strings.TrimSpace(prompt) != "" {
		if _, err := a.StartTurnWithPrompt(ctx, threadResp.Thread.ID, prompt); err != nil {
			return SessionSummary{}, err
		}
	}

	record, _ := a.store.SnapshotSession(threadResp.Thread.ID)
	summary := toSessionSummary(record, 0)
	a.broker.Publish("session.created", summary)
	return summary, nil
}

func (a *Agent) ResumeSession(ctx context.Context, threadID string) (SessionSummary, error) {
	var response codex.ThreadResumeResponse
	if err := a.client.Call(ctx, "thread/resume", map[string]any{
		"threadId":               threadID,
		"persistExtendedHistory": true,
	}, &response); err != nil {
		return SessionSummary{}, err
	}

	a.store.UpsertThread(response.Thread)
	a.store.SetSessionEnded(threadID, false)
	a.store.SetSessionManaged(threadID, true)
	a.store.SetSessionLoaded(threadID, true)
	record, _ := a.store.SnapshotSession(threadID)
	summary := toSessionSummary(record, 0)
	a.broker.Publish("session.resumed", summary)
	return summary, nil
}

func (a *Agent) EndSession(ctx context.Context, threadID string) error {
	record, ok := a.store.SnapshotSession(threadID)
	if ok && record.Loaded && len(record.Thread.Turns) > 0 {
		lastTurn := record.Thread.Turns[len(record.Thread.Turns)-1]
		if lastTurn.Status == "inProgress" {
			if err := a.InterruptTurn(ctx, threadID, lastTurn.ID); err != nil {
				return err
			}
		}
	}

	var response codex.ThreadUnsubscribeResponse
	if err := a.client.Call(ctx, "thread/unsubscribe", map[string]any{
		"threadId": threadID,
	}, &response); err != nil {
		return err
	}

	switch response.Status {
	case "", "unsubscribed", "notSubscribed", "notLoaded":
	default:
		return fmt.Errorf("unexpected unsubscribe status %q", response.Status)
	}

	a.store.SetSessionEnded(threadID, true)
	a.store.SetSessionManaged(threadID, false)
	a.store.SetSessionLoaded(threadID, false)
	_ = a.Refresh(ctx)
	a.broker.Publish("session.ended", map[string]string{
		"threadId": threadID,
	})
	return nil
}

func (a *Agent) ArchiveSession(ctx context.Context, threadID string) error {
	if err := a.client.Call(ctx, "thread/archive", map[string]any{
		"threadId": threadID,
	}, nil); err != nil {
		return err
	}

	a.store.DeleteSessionLocalState(threadID)
	_ = a.Refresh(ctx)
	a.broker.Publish("session.archived", map[string]string{
		"threadId": threadID,
	})
	return nil
}

func (a *Agent) StartTurnWithPrompt(ctx context.Context, threadID, prompt string) (TurnDetail, error) {
	return a.StartTurn(ctx, threadID, []map[string]any{textInput(prompt)})
}

func (a *Agent) StartTurn(ctx context.Context, threadID string, input []map[string]any) (TurnDetail, error) {
	if len(input) == 0 {
		return TurnDetail{}, errors.New("turn input is required")
	}

	a.appendTurnInputLog(threadID, "", "start", input)

	var response codex.TurnStartResponse
	if err := a.client.Call(ctx, "turn/start", map[string]any{
		"threadId": threadID,
		"input":    input,
	}, &response); err != nil {
		return TurnDetail{}, err
	}

	a.store.SetSessionEnded(threadID, false)
	a.store.RecordTurn(threadID, response.Turn)
	a.broker.Publish("turn.started", map[string]string{
		"threadId": threadID,
		"turnId":   response.Turn.ID,
	})

	record, _ := a.store.SnapshotSession(threadID)
	for _, turn := range toSessionDetail(record, 0, a.files).Turns {
		if turn.ID == response.Turn.ID {
			return turn, nil
		}
	}
	return TurnDetail{}, errors.New("turn not found after start")
}

func (a *Agent) SteerTurnWithPrompt(ctx context.Context, threadID, turnID, prompt string) error {
	return a.SteerTurn(ctx, threadID, turnID, []map[string]any{textInput(prompt)})
}

func (a *Agent) SteerTurn(ctx context.Context, threadID, turnID string, input []map[string]any) error {
	if len(input) == 0 {
		return errors.New("turn input is required")
	}

	a.appendTurnInputLog(threadID, turnID, "steer", input)

	var response codex.TurnSteerResponse
	if err := a.client.Call(ctx, "turn/steer", map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input":          input,
	}, &response); err != nil {
		return err
	}

	a.broker.Publish("turn.steered", map[string]string{
		"threadId": threadID,
		"turnId":   turnID,
	})
	return nil
}

func (a *Agent) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	var response codex.TurnInterruptResponse
	if err := a.client.Call(ctx, "turn/interrupt", map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
	}, &response); err != nil {
		return err
	}
	a.broker.Publish("turn.interrupted", map[string]string{
		"threadId": threadID,
		"turnId":   turnID,
	})
	return nil
}

func (a *Agent) consumeNotifications(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case notification := <-a.client.Notifications():
			a.handleNotification(ctx, notification)
		}
	}
}

func (a *Agent) consumeServerRequests(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-a.client.ServerRequests():
			a.handleServerRequest(ctx, request)
		}
	}
}

func (a *Agent) consumeStderr() {
	for line := range a.client.StderrLines() {
		a.logger.Debug("codex app-server stderr", "line", line)
	}
}

func (a *Agent) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.Refresh(ctx); err != nil {
				a.logger.Warn("periodic refresh failed", "error", err)
			}
		}
	}
}

func (a *Agent) fetchThreads(ctx context.Context) ([]codex.Thread, error) {
	var all []codex.Thread
	var cursor *string

	for {
		params := map[string]any{
			"useStateDbOnly": false,
			// Codex defaults sourceKinds to newer interactive-only values. Including
			// "unknown" keeps older local sessions with empty thread_source visible.
			"sourceKinds": append([]string(nil), dashboardThreadSourceKinds...),
			// A present-but-empty provider filter means "all model providers".
			"modelProviders": []string{},
			"limit":          500,
		}
		if cursor != nil {
			params["cursor"] = *cursor
		}

		var response codex.ThreadListResponse
		if err := a.client.Call(ctx, "thread/list", params, &response); err != nil {
			return nil, err
		}

		all = append(all, response.Data...)
		if response.NextCursor == nil || *response.NextCursor == "" {
			break
		}
		cursor = response.NextCursor
	}

	return all, nil
}

func (a *Agent) fetchLoadedThreadIDs(ctx context.Context) ([]string, error) {
	var all []string
	var cursor *string

	for {
		params := map[string]any{}
		if cursor != nil {
			params["cursor"] = *cursor
		}

		var response codex.ThreadLoadedListResponse
		if err := a.client.Call(ctx, "thread/loaded/list", params, &response); err != nil {
			return nil, err
		}

		all = append(all, response.Data...)
		if response.NextCursor == nil || *response.NextCursor == "" {
			break
		}
		cursor = response.NextCursor
	}

	return all, nil
}

func (a *Agent) handleNotification(ctx context.Context, notification codex.Notification) {
	switch notification.Method {
	case "thread/started":
		var payload codex.ThreadStartedNotification
		if json.Unmarshal(notification.Params, &payload) == nil {
			a.store.UpsertThread(payload.Thread)
		}
	case "thread/status/changed":
		var payload codex.ThreadStatusChangedNotification
		if json.Unmarshal(notification.Params, &payload) == nil {
			a.store.UpdateThreadStatus(payload.ThreadID, payload.Status)
		}
	case "turn/started":
		var payload codex.TurnStartedNotification
		if json.Unmarshal(notification.Params, &payload) == nil {
			a.store.RecordTurn(payload.ThreadID, payload.Turn)
		}
	case "turn/completed":
		var payload codex.TurnCompletedNotification
		if json.Unmarshal(notification.Params, &payload) == nil {
			a.store.RecordTurn(payload.ThreadID, payload.Turn)
			a.appendTurnCompletedLog(payload.ThreadID, payload.Turn)
		}
	case "turn/diff/updated":
		var payload codex.TurnDiffUpdatedNotification
		if json.Unmarshal(notification.Params, &payload) == nil {
			a.store.RecordDiff(payload.ThreadID, payload.TurnID, payload.Diff)
		}
	case "turn/plan/updated":
		var payload codex.TurnPlanUpdatedNotification
		if json.Unmarshal(notification.Params, &payload) == nil {
			a.store.RecordPlan(payload)
		}
	case "thread/closed":
		_ = a.Refresh(ctx)
	}

	a.broker.Publish("codex.notification", map[string]any{
		"method": notification.Method,
		"params": json.RawMessage(notification.Params),
	})
}

func (a *Agent) handleServerRequest(ctx context.Context, request codex.ServerRequest) {
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		a.logger.Warn("failed to decode server request params", "method", request.Method, "error", err)
		return
	}

	choices := deriveChoices(request.Method, params)
	pending := a.store.UpsertPending(request.Method, request.ID, params, choices)
	a.broker.Publish("approval.created", PendingRequestView{
		ID:        pending.ID,
		Method:    pending.Method,
		Kind:      requestKind(pending.Method),
		ThreadID:  pending.ThreadID,
		TurnID:    pending.TurnID,
		ItemID:    pending.ItemID,
		Reason:    pending.Reason,
		Summary:   pending.Summary,
		Choices:   cloneStrings(pending.Choices),
		CreatedAt: pending.CreatedAt,
		Params:    pending.Params,
	})
}

func deriveChoices(method string, params map[string]any) []string {
	switch method {
	case "item/commandExecution/requestApproval":
		if raw, ok := params["availableDecisions"].([]any); ok && len(raw) > 0 {
			var choices []string
			for _, item := range raw {
				switch value := item.(type) {
				case string:
					choices = append(choices, value)
				case map[string]any:
					for key := range value {
						choices = append(choices, key)
					}
				}
			}
			if len(choices) > 0 {
				return choices
			}
		}
		return []string{"accept", "acceptForSession", "decline", "cancel"}
	case "item/fileChange/requestApproval":
		return []string{"accept", "acceptForSession", "decline", "cancel"}
	case "item/permissions/requestApproval":
		return []string{"session", "turn", "decline"}
	case "item/tool/requestUserInput":
		return []string{"answer"}
	default:
		return []string{"accept", "decline"}
	}
}

func emptyToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func textInput(prompt string) map[string]any {
	return map[string]any{
		"type":          "text",
		"text":          prompt,
		"text_elements": []any{},
	}
}

func (a *Agent) appendTurnInputLog(threadID, turnID, action string, input []map[string]any) {
	if a.worklog == nil {
		return
	}
	prompt := textFromInput(input)
	if strings.TrimSpace(prompt) == "" {
		prompt = fmt.Sprintf("[%d 个非文本输入]", len(input))
	}
	summary := []string{
		fmt.Sprintf("CodexFlow API 已发送 %s 请求到会话 %s。", action, threadID),
	}
	if strings.TrimSpace(turnID) != "" {
		summary = append(summary, "目标 turn: "+turnID)
	}
	_ = a.worklog.Append(worklog.Entry{
		Title:       "手机端 CodexFlow 输入同步",
		UserRequest: worklog.Truncate(prompt, 4000),
		Assistant:   "输入已同步到本机 CodexFlow Agent，并将由 codex app-server 继续处理。",
		Summary:     summary,
		ChangedFiles: []string{
			a.worklog.PathForToday(),
		},
		Verification: []string{"已在发送 turn 请求前写入本地工作记录。"},
	})
}

func (a *Agent) appendTurnCompletedLog(threadID string, turn codex.Turn) {
	if a.worklog == nil {
		return
	}
	record, ok := a.store.SnapshotSession(threadID)
	if !ok {
		return
	}
	detail := toTurnDetail(turn, record.Runtime, a.files, record.Thread.CWD)
	userText, agentText := turnDialogSummary(detail)
	artifactLines := artifactSummary(detail)
	summary := []string{
		fmt.Sprintf("CodexFlow turn 已完成：thread=%s turn=%s status=%s。", threadID, turn.ID, turn.Status),
	}
	summary = append(summary, artifactLines...)
	_ = a.worklog.Append(worklog.Entry{
		Title:       "手机端 CodexFlow 输出同步",
		UserRequest: worklog.Truncate(userText, 4000),
		Assistant:   worklog.Truncate(agentText, 4000),
		Summary:     summary,
		ChangedFiles: []string{
			a.worklog.PathForToday(),
		},
		Verification: []string{"已从 turn/completed 通知写入本地工作记录。"},
	})
}

func textFromInput(input []map[string]any) string {
	parts := make([]string, 0, len(input))
	for _, item := range input {
		itemType, _ := item["type"].(string)
		switch itemType {
		case "text":
			if text, ok := item["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		case "localImage":
			if path, ok := item["path"].(string); ok && strings.TrimSpace(path) != "" {
				parts = append(parts, "[图片] "+strings.TrimSpace(path))
			}
		default:
			parts = append(parts, fmt.Sprintf("[%s 输入]", itemType))
		}
	}
	return strings.Join(parts, "\n\n")
}

func turnDialogSummary(turn TurnDetail) (string, string) {
	var userText string
	var agentText string
	for _, item := range turn.Items {
		if item.Type == "userMessage" && strings.TrimSpace(item.Body) != "" && userText == "" {
			userText = item.Body
		}
		if item.Type == "agentMessage" && strings.TrimSpace(item.Body) != "" {
			agentText = item.Body
		}
	}
	if userText == "" {
		userText = "无用户文本输入。"
	}
	if agentText == "" {
		agentText = fmt.Sprintf("Turn 已结束，状态：%s。", turn.Status)
	}
	return userText, agentText
}

func artifactSummary(turn TurnDetail) []string {
	seen := make(map[string]struct{})
	lines := make([]string, 0)
	for _, item := range turn.Items {
		for _, artifact := range item.Artifacts {
			if _, ok := seen[artifact.ID]; ok {
				continue
			}
			seen[artifact.ID] = struct{}{}
			lines = append(lines, fmt.Sprintf("可手机打开产物：%s (%s)", artifact.Name, artifact.DownloadURL))
		}
	}
	if len(lines) == 0 {
		return []string{"本轮未识别到可手机打开的产物。"}
	}
	return lines
}

func pendingCountForThread(pending []store.PendingRequest, threadID string) int {
	count := 0
	for _, item := range pending {
		if item.ThreadID == threadID {
			count++
		}
	}
	return count
}
