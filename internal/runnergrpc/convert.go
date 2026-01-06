package runnergrpc

import (
	"pkt.systems/centaurx/core"
	"pkt.systems/centaurx/internal/runnerpb"
	"pkt.systems/centaurx/schema"
)

func toPBExecEvent(event schema.ExecEvent) *runnerpb.ExecEvent {
	out := &runnerpb.ExecEvent{
		Type:    toPBEventType(event.Type),
		Message: event.Message,
		Raw:     event.Raw,
	}
	if event.ThreadID != "" {
		out.ThreadId = string(event.ThreadID)
	}
	if event.Usage != nil {
		out.Usage = &runnerpb.TurnUsage{
			InputTokens:       int32(event.Usage.InputTokens),
			CachedInputTokens: int32(event.Usage.CachedInputTokens),
			OutputTokens:      int32(event.Usage.OutputTokens),
		}
	}
	if event.Item != nil {
		out.Item = toPBItemEvent(event.Item)
	}
	if event.Error != nil {
		out.Error = &runnerpb.ErrorEvent{Message: event.Error.Message}
	}
	return out
}

func fromPBExecEvent(event *runnerpb.ExecEvent) schema.ExecEvent {
	out := schema.ExecEvent{
		Type:    fromPBEventType(event.Type),
		Message: event.Message,
		Raw:     event.Raw,
	}
	if event.ThreadId != "" {
		out.ThreadID = schema.SessionID(event.ThreadId)
	}
	if event.Usage != nil {
		out.Usage = &schema.TurnUsage{
			InputTokens:       int(event.Usage.InputTokens),
			CachedInputTokens: int(event.Usage.CachedInputTokens),
			OutputTokens:      int(event.Usage.OutputTokens),
		}
	}
	if event.Item != nil {
		out.Item = fromPBItemEvent(event.Item)
	}
	if event.Error != nil {
		out.Error = &schema.ErrorEvent{Message: event.Error.Message}
	}
	return out
}

func toPBItemEvent(item *schema.ItemEvent) *runnerpb.ItemEvent {
	out := &runnerpb.ItemEvent{
		Id:               item.ID,
		Type:             toPBItemType(item.Type),
		Text:             item.Text,
		Command:          item.Command,
		AggregatedOutput: item.AggregatedOutput,
		Status:           item.Status,
		Query:            item.Query,
		Raw:              item.Raw,
	}
	if item.ExitCode != nil {
		val := int32(*item.ExitCode)
		out.ExitCode = &val
	}
	if len(item.Changes) > 0 {
		out.Changes = make([]*runnerpb.FileChange, 0, len(item.Changes))
		for _, change := range item.Changes {
			out.Changes = append(out.Changes, &runnerpb.FileChange{Path: change.Path, Kind: change.Kind})
		}
	}
	if len(item.Items) > 0 {
		out.Items = make([]*runnerpb.TodoItem, 0, len(item.Items))
		for _, entry := range item.Items {
			out.Items = append(out.Items, &runnerpb.TodoItem{Text: entry.Text, Completed: entry.Completed})
		}
	}
	return out
}

func fromPBItemEvent(item *runnerpb.ItemEvent) *schema.ItemEvent {
	out := &schema.ItemEvent{
		ID:               item.Id,
		Type:             fromPBItemType(item.Type),
		Text:             item.Text,
		Command:          item.Command,
		AggregatedOutput: item.AggregatedOutput,
		Status:           item.Status,
		Query:            item.Query,
		Raw:              item.Raw,
	}
	if item.ExitCode != nil {
		val := int(*item.ExitCode)
		out.ExitCode = &val
	}
	if len(item.Changes) > 0 {
		out.Changes = make([]schema.FileChange, 0, len(item.Changes))
		for _, change := range item.Changes {
			out.Changes = append(out.Changes, schema.FileChange{Path: change.Path, Kind: change.Kind})
		}
	}
	if len(item.Items) > 0 {
		out.Items = make([]schema.TodoItem, 0, len(item.Items))
		for _, entry := range item.Items {
			out.Items = append(out.Items, schema.TodoItem{Text: entry.Text, Completed: entry.Completed})
		}
	}
	return out
}

func toPBEventType(value schema.EventType) runnerpb.EventType {
	switch value {
	case schema.EventThreadStarted:
		return runnerpb.EventType_EVENT_THREAD_STARTED
	case schema.EventTurnStarted:
		return runnerpb.EventType_EVENT_TURN_STARTED
	case schema.EventTurnCompleted:
		return runnerpb.EventType_EVENT_TURN_COMPLETED
	case schema.EventTurnFailed:
		return runnerpb.EventType_EVENT_TURN_FAILED
	case schema.EventItemStarted:
		return runnerpb.EventType_EVENT_ITEM_STARTED
	case schema.EventItemUpdated:
		return runnerpb.EventType_EVENT_ITEM_UPDATED
	case schema.EventItemCompleted:
		return runnerpb.EventType_EVENT_ITEM_COMPLETED
	case schema.EventError:
		return runnerpb.EventType_EVENT_ERROR
	default:
		return runnerpb.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

func fromPBEventType(value runnerpb.EventType) schema.EventType {
	switch value {
	case runnerpb.EventType_EVENT_THREAD_STARTED:
		return schema.EventThreadStarted
	case runnerpb.EventType_EVENT_TURN_STARTED:
		return schema.EventTurnStarted
	case runnerpb.EventType_EVENT_TURN_COMPLETED:
		return schema.EventTurnCompleted
	case runnerpb.EventType_EVENT_TURN_FAILED:
		return schema.EventTurnFailed
	case runnerpb.EventType_EVENT_ITEM_STARTED:
		return schema.EventItemStarted
	case runnerpb.EventType_EVENT_ITEM_UPDATED:
		return schema.EventItemUpdated
	case runnerpb.EventType_EVENT_ITEM_COMPLETED:
		return schema.EventItemCompleted
	case runnerpb.EventType_EVENT_ERROR:
		return schema.EventError
	default:
		return schema.EventType("")
	}
}

func toPBItemType(value schema.ItemType) runnerpb.ItemType {
	switch value {
	case schema.ItemAgentMessage:
		return runnerpb.ItemType_ITEM_AGENT_MESSAGE
	case schema.ItemReasoning:
		return runnerpb.ItemType_ITEM_REASONING
	case schema.ItemCommandExecution:
		return runnerpb.ItemType_ITEM_COMMAND_EXECUTION
	case schema.ItemFileChange:
		return runnerpb.ItemType_ITEM_FILE_CHANGE
	case schema.ItemMcpToolCall:
		return runnerpb.ItemType_ITEM_MCP_TOOL_CALL
	case schema.ItemWebSearch:
		return runnerpb.ItemType_ITEM_WEB_SEARCH
	case schema.ItemTodoList:
		return runnerpb.ItemType_ITEM_TODO_LIST
	case schema.ItemError:
		return runnerpb.ItemType_ITEM_ERROR
	default:
		return runnerpb.ItemType_ITEM_TYPE_UNSPECIFIED
	}
}

func fromPBItemType(value runnerpb.ItemType) schema.ItemType {
	switch value {
	case runnerpb.ItemType_ITEM_AGENT_MESSAGE:
		return schema.ItemAgentMessage
	case runnerpb.ItemType_ITEM_REASONING:
		return schema.ItemReasoning
	case runnerpb.ItemType_ITEM_COMMAND_EXECUTION:
		return schema.ItemCommandExecution
	case runnerpb.ItemType_ITEM_FILE_CHANGE:
		return schema.ItemFileChange
	case runnerpb.ItemType_ITEM_MCP_TOOL_CALL:
		return schema.ItemMcpToolCall
	case runnerpb.ItemType_ITEM_WEB_SEARCH:
		return schema.ItemWebSearch
	case runnerpb.ItemType_ITEM_TODO_LIST:
		return schema.ItemTodoList
	case runnerpb.ItemType_ITEM_ERROR:
		return schema.ItemError
	default:
		return schema.ItemType("")
	}
}

func toPBUsageWindow(window *core.UsageWindow) *runnerpb.UsageWindow {
	if window == nil {
		return nil
	}
	return &runnerpb.UsageWindow{
		UsedPercent:        window.UsedPercent,
		LimitWindowSeconds: window.LimitWindowSeconds,
		ResetAt:            window.ResetAt,
	}
}

func fromPBUsageWindow(window *runnerpb.UsageWindow) *core.UsageWindow {
	if window == nil {
		return nil
	}
	return &core.UsageWindow{
		UsedPercent:        window.UsedPercent,
		LimitWindowSeconds: window.LimitWindowSeconds,
		ResetAt:            window.ResetAt,
	}
}
