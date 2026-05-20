package runtime

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"codexflow/internal/artifacts"
	"codexflow/internal/codex"
	"codexflow/internal/store"
)

const (
	defaultSessionDetailTurnLimit = 12
	defaultTurnItemLimit          = 48
	defaultTextHeadLimit          = 2000
	defaultTextTailLimit          = 1200
	defaultDiffHeadLimit          = 4000
	defaultDiffTailLimit          = 2000
)

type ArtifactResolver interface {
	ResolveTextArtifacts(text, cwd string) []artifacts.Ref
}

type sessionDetailOptions struct {
	TurnLimit     int
	ItemLimit     int
	TextHeadLimit int
	TextTailLimit int
	DiffHeadLimit int
	DiffTailLimit int
}

func defaultSessionDetailOptions() sessionDetailOptions {
	return sessionDetailOptions{
		TurnLimit:     defaultSessionDetailTurnLimit,
		ItemLimit:     defaultTurnItemLimit,
		TextHeadLimit: defaultTextHeadLimit,
		TextTailLimit: defaultTextTailLimit,
		DiffHeadLimit: defaultDiffHeadLimit,
		DiffTailLimit: defaultDiffTailLimit,
	}
}

// Mobile clients render every returned turn/item eagerly, so keep the default
// detail response focused on recent activity and leave full history opt-in.
func fullSessionDetailOptions() sessionDetailOptions {
	return sessionDetailOptions{}
}

func toSessionSummary(record store.SessionRecord, pendingApprovals int) SessionSummary {
	var lastTurnID string
	var lastTurnStatus string
	if len(record.Thread.Turns) > 0 {
		lastTurn := record.Thread.Turns[len(record.Thread.Turns)-1]
		lastTurnID = lastTurn.ID
		lastTurnStatus = lastTurn.Status
	}

	effectiveLoaded := record.Loaded && !record.Runtime.Ended

	return SessionSummary{
		ID:               record.Thread.ID,
		Name:             optionalString(record.Thread.Name),
		Preview:          record.Thread.Preview,
		CWD:              record.Thread.CWD,
		Source:           codex.SourceLabel(record.Thread.Source),
		Status:           record.Thread.Status.Type,
		ActiveFlags:      cloneStrings(record.Thread.Status.ActiveFlags),
		Loaded:           effectiveLoaded,
		UpdatedAt:        record.Thread.UpdatedAt,
		CreatedAt:        record.Thread.CreatedAt,
		ModelProvider:    record.Thread.ModelProvider,
		Branch:           codex.GitBranch(record.Thread.GitInfo),
		PendingApprovals: pendingApprovals,
		LastTurnID:       lastTurnID,
		LastTurnStatus:   lastTurnStatus,
		AgentNickname:    optionalString(record.Thread.AgentNickname),
		AgentRole:        optionalString(record.Thread.AgentRole),
		Ended:            record.Runtime.Ended,
	}
}

func toSessionDetail(record store.SessionRecord, pendingApprovals int, resolver ArtifactResolver) SessionDetail {
	return toSessionDetailWithOptions(record, pendingApprovals, resolver, defaultSessionDetailOptions())
}

func toFullSessionDetail(record store.SessionRecord, pendingApprovals int, resolver ArtifactResolver) SessionDetail {
	return toSessionDetailWithOptions(record, pendingApprovals, resolver, fullSessionDetailOptions())
}

func toSessionDetailWithOptions(record store.SessionRecord, pendingApprovals int, resolver ArtifactResolver, opts sessionDetailOptions) SessionDetail {
	sourceTurns := record.Thread.Turns
	totalItems := 0
	for _, turn := range sourceTurns {
		totalItems += len(turn.Items)
	}

	start := 0
	if opts.TurnLimit > 0 && len(sourceTurns) > opts.TurnLimit {
		start = len(sourceTurns) - opts.TurnLimit
	}

	turns := make([]TurnDetail, 0, len(sourceTurns)-start)
	omittedItems := 0
	for _, turn := range sourceTurns[:start] {
		omittedItems += len(turn.Items)
	}
	for _, turn := range sourceTurns[start:] {
		detail := toTurnDetailWithOptions(turn, record.Runtime, resolver, record.Thread.CWD, opts)
		omittedItems += detail.OmittedItems
		turns = append(turns, detail)
	}

	omittedTurns := start
	return SessionDetail{
		Summary:      toSessionSummary(record, pendingApprovals),
		Turns:        turns,
		TotalTurns:   len(sourceTurns),
		OmittedTurns: omittedTurns,
		TotalItems:   totalItems,
		OmittedItems: omittedItems,
		Limited:      omittedTurns > 0 || omittedItems > 0,
	}
}

func toTurnDetail(turn codex.Turn, runtimeState store.SessionRuntime, resolver ArtifactResolver, cwd string) TurnDetail {
	return toTurnDetailWithOptions(turn, runtimeState, resolver, cwd, fullSessionDetailOptions())
}

func toTurnDetailWithOptions(turn codex.Turn, runtimeState store.SessionRuntime, resolver ArtifactResolver, cwd string, opts sessionDetailOptions) TurnDetail {
	sourceItems := limitedTurnItems(turn.Items, opts.ItemLimit)
	detail := TurnDetail{
		ID:          turn.ID,
		Status:      turn.Status,
		Diff:        summarizeLongText(runtimeState.LatestDiffByTurn[turn.ID], opts.DiffHeadLimit, opts.DiffTailLimit),
		Plan:        make([]PlanStep, 0),
		Items:       make([]TurnItem, 0, len(sourceItems)),
		StartedAt:   derefInt64(turn.StartedAt),
		CompletedAt: derefInt64(turn.CompletedAt),
		DurationMs:  derefInt64(turn.DurationMs),
		TotalItems:  len(turn.Items),
	}
	detail.OmittedItems = len(turn.Items) - len(sourceItems)
	detail.Limited = detail.OmittedItems > 0

	if turn.Error != nil {
		detail.Error = turn.Error.Message
	}

	if plan, ok := runtimeState.LatestPlanByTurn[turn.ID]; ok {
		detail.PlanExplanation = summarizeLongText(optionalString(plan.Explanation), opts.TextHeadLimit, opts.TextTailLimit)
		detail.Plan = make([]PlanStep, 0, len(plan.Plan))
		for _, step := range plan.Plan {
			detail.Plan = append(detail.Plan, PlanStep{Step: step.Step, Status: step.Status})
		}
	}

	for _, item := range sourceItems {
		detail.Items = append(detail.Items, normalizeItemWithOptions(item, resolver, cwd, opts))
	}

	return detail
}

func normalizeItem(item map[string]any, resolver ArtifactResolver, sessionCWD string) TurnItem {
	return normalizeItemWithOptions(item, resolver, sessionCWD, fullSessionDetailOptions())
}

func normalizeItemWithOptions(item map[string]any, resolver ArtifactResolver, sessionCWD string, opts sessionDetailOptions) TurnItem {
	itemType, _ := item["type"].(string)
	id, _ := item["id"].(string)

	result := TurnItem{
		ID:       id,
		Type:     itemType,
		Metadata: map[string]string{},
	}

	switch itemType {
	case "userMessage":
		result.Title = "User Prompt"
		result.Body = codex.FirstUserText([]map[string]any{item})
	case "agentMessage":
		result.Title = "Agent"
		result.Body, _ = item["text"].(string)
	case "plan":
		result.Title = "Plan"
		result.Body, _ = item["text"].(string)
	case "reasoning":
		result.Title = "Reasoning"
		if summary, ok := item["summary"].([]any); ok {
			result.Body = joinAny(summary, "\n")
		}
	case "commandExecution":
		result.Title = "Command"
		result.Body, _ = item["command"].(string)
		result.Status, _ = item["status"].(string)
		if output, ok := item["aggregatedOutput"].(string); ok {
			result.Auxiliary = output
		}
		if cwd, ok := item["cwd"].(string); ok {
			result.Metadata["cwd"] = cwd
		}
	case "fileChange":
		result.Title = "File Change"
		result.Status, _ = item["status"].(string)
		if changes, ok := item["changes"].([]any); ok {
			files := summarizeFileChanges(changes)
			if len(files) > 0 {
				result.Body = strings.Join(files, "\n")
				result.Metadata["changeCount"] = fmt.Sprintf("%d", len(files))
			} else {
				result.Body = fmt.Sprintf("%d file changes", len(changes))
				result.Metadata["changeCount"] = fmt.Sprintf("%d", len(changes))
			}
		}
	case "mcpToolCall":
		result.Title = "MCP Tool"
		result.Body = fmt.Sprintf("%v/%v", item["server"], item["tool"])
	case "dynamicToolCall":
		result.Title = "Tool Call"
		result.Body = fmt.Sprintf("%v:%v", item["namespace"], item["tool"])
	case "collabAgentToolCall":
		result.Title = "Delegation"
		result.Body, _ = item["prompt"].(string)
		result.Status, _ = item["status"].(string)
	default:
		result.Title = strings.Title(itemType)
	}

	if result.Body == "" {
		result.Body = summarizeUnknown(item)
	}
	result.Body = summarizeLongText(result.Body, opts.TextHeadLimit, opts.TextTailLimit)
	result.Auxiliary = summarizeLongText(result.Auxiliary, opts.TextHeadLimit, opts.TextTailLimit)

	if resolver != nil {
		cwd := result.Metadata["cwd"]
		if cwd == "" {
			cwd = sessionCWD
		}
		result.Artifacts = resolver.ResolveTextArtifacts(result.Body+"\n"+result.Auxiliary, cwd)
	}
	return result
}

func limitedTurnItems(items []map[string]any, limit int) []map[string]any {
	if limit <= 0 || len(items) <= limit {
		return items
	}

	keep := make([]map[string]any, 0, limit)
	firstUserIdx := -1
	for idx, item := range items {
		if itemType, _ := item["type"].(string); itemType == "userMessage" {
			firstUserIdx = idx
			break
		}
	}

	start := len(items) - limit
	includeFirstUser := firstUserIdx >= 0 && firstUserIdx < start
	if includeFirstUser {
		keep = append(keep, items[firstUserIdx])
		start++
	}
	keep = append(keep, items[start:]...)
	return keep
}

func summarizeLongText(text string, headLimit, tailLimit int) string {
	if text == "" || headLimit <= 0 || tailLimit <= 0 {
		return text
	}

	runes := utf8.RuneCountInString(text)
	limit := headLimit + tailLimit
	if runes <= limit {
		return text
	}

	head := firstRunes(text, headLimit)
	tail := lastRunes(text, tailLimit)
	omitted := runes - limit
	return fmt.Sprintf("%s\n\n... omitted %d characters ...\n\n%s", head, omitted, tail)
}

func firstRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	count := 0
	for idx := range text {
		if count == limit {
			return text[:idx]
		}
		count++
	}
	return text
}

func lastRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := utf8.RuneCountInString(text)
	if runes <= limit {
		return text
	}
	startRune := runes - limit
	count := 0
	for idx := range text {
		if count == startRune {
			return text[idx:]
		}
		count++
	}
	return text
}

func summarizeUnknown(item map[string]any) string {
	parts := make([]string, 0, 4)
	for _, key := range []string{"status", "review", "result"} {
		if text, ok := item[key].(string); ok && text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " · ")
}

func joinAny(values []any, sep string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, sep)
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string{}, values...)
}

func summarizeFileChanges(changes []any) []string {
	files := make([]string, 0, len(changes))
	seen := make(map[string]struct{}, len(changes))

	for _, change := range changes {
		changeMap, ok := change.(map[string]any)
		if !ok {
			continue
		}

		oldPath := stringFieldAny(changeMap, "oldPath")
		newPath := stringFieldAny(changeMap, "newPath")
		if oldPath != "" && newPath != "" && oldPath != newPath {
			addUniqueFile(&files, seen, fmt.Sprintf("%s -> %s", oldPath, newPath))
			continue
		}

		for _, key := range []string{"path", "filePath", "relativePath", "newPath", "oldPath"} {
			if value := stringFieldAny(changeMap, key); value != "" {
				addUniqueFile(&files, seen, value)
				break
			}
		}
	}

	return files
}

func stringFieldAny(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func addUniqueFile(files *[]string, seen map[string]struct{}, value string) {
	if value == "" {
		return
	}
	if _, ok := seen[value]; ok {
		return
	}
	seen[value] = struct{}{}
	*files = append(*files, value)
}

func requestKind(method string) string {
	switch method {
	case "item/commandExecution/requestApproval":
		return "command"
	case "item/fileChange/requestApproval":
		return "fileChange"
	case "item/permissions/requestApproval":
		return "permissions"
	case "item/tool/requestUserInput":
		return "userInput"
	default:
		return "generic"
	}
}
