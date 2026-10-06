package server

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"remote-mcp/internal/execution"
	"remote-mcp/internal/forwarding"
	"remote-mcp/internal/gui"
	"remote-mcp/internal/transfer"
)

const (
	maxDiagnosticMessageBytes = 1024
	maxBusinessMessageBytes   = 4096
	maxDiagnosticRequestBytes = 1 << 20
)

// 只有项目已有工具域的安全消息契约受信任；未来自定义工具不自动获得原文日志权限。
func toolDomain(name string) string {
	switch name {
	case "gui_status", "gui_open", "gui_close", "gui_screenshot", "gui_mouse", "gui_key", "gui_text":
		return "gui"
	case "file_stat", "upload_create", "upload_write", "upload_finish", "upload_cancel", "download_open", "download_read", "download_close":
		return "transfer"
	case "process_start", "process_read", "process_status", "process_stop", "terminal_open", "terminal_write", "terminal_read", "terminal_resize", "terminal_status", "terminal_close":
		return "execution"
	case "port_forward_create", "port_forward_list", "port_forward_status", "port_forward_stop":
		return "forwarding"
	}
	return ""
}

// 新增业务错误码时须同步此日志边界；未知码不带入原始错误说明。
func knownBusinessCode(domain, code string) bool {
	codes := map[string]string{
		"gui":        "invalid_argument unsupported no_gui permission_denied dependency_missing authorization_cancelled authorization_timeout session_closed not_found conflict busy stale_capture capture_failed timeout limit_exceeded input_failed clipboard_preservation_unavailable clipboard_restore_failed",
		"transfer":   "CANCELED CHECKSUM_MISMATCH CLOSED CONFLICT INVALID_ARGUMENT INVALID_OFFSET INVALID_STATE IO_ERROR LIMIT_EXCEEDED NOT_FOUND PERMISSION_DENIED SOURCE_CHANGED",
		"execution":  "invalid_argument closed conflict resource_limit internal io_error not_found invalid_state busy interrupted timeout start_failed",
		"forwarding": "invalid_argument closed conflict resource_limit internal not_found listen_failed",
	}
	for _, known := range strings.Fields(codes[domain]) {
		if known == code {
			return true
		}
	}
	return false
}

type toolDiagnostic struct {
	code, message string
	guiError      *gui.Error
}

func businessDiagnostic(domain string, call *mcp.CallToolResult, err error) (toolDiagnostic, bool) {
	var diagnostic toolDiagnostic
	switch domain {
	case "gui":
		var business *gui.Error
		if call != nil {
			business, _ = call.StructuredContent.(*gui.Error)
		}
		if business == nil {
			errors.As(err, &business)
		}
		if business != nil {
			diagnostic = toolDiagnostic{business.Code, business.Message, business}
		}
	case "execution":
		var business *execution.Error
		if errors.As(err, &business) && business != nil {
			diagnostic.code, diagnostic.message = business.Code, business.Message
		}
	case "forwarding":
		var business *forwarding.Error
		if errors.As(err, &business) && business != nil {
			diagnostic.code, diagnostic.message = business.Code, business.Message
		}
	case "transfer":
		if call == nil {
			break
		}
		// SDK 将注册器的 typed transfer.Result 输出序列化为 RawMessage；不读取文本 Content。
		var business transfer.Result
		switch output := call.StructuredContent.(type) {
		case transfer.Result:
			business = output
		case *transfer.Result:
			if output != nil {
				business = *output
			}
		case json.RawMessage:
			if len(output) <= maxBusinessMessageBytes {
				var projected struct {
					OK      *bool  `json:"ok"`
					Code    string `json:"code"`
					Message string `json:"message"`
				}
				if json.Unmarshal(output, &projected) == nil && projected.OK != nil && !*projected.OK {
					business.Code, business.Message = projected.Code, projected.Message
				}
			}
		}
		if !business.OK {
			diagnostic.code, diagnostic.message = business.Code, business.Message
		}
	}
	return diagnostic, knownBusinessCode(domain, diagnostic.code)
}

func toolErrorAttributes(req mcp.Request, result mcp.Result, err error, token string) []any {
	var tool string
	var arguments json.RawMessage
	if request, ok := req.(*mcp.CallToolRequest); ok && request != nil && request.Params != nil {
		tool, arguments = request.Params.Name, request.Params.Arguments
	}
	call, _ := result.(*mcp.CallToolResult)
	if err == nil && call != nil {
		// GetError 仅服务端可见，能保留被 SDK 转为 IsError 的原始业务类型与取消状态。
		err = call.GetError()
	}
	diagnostic, trusted := businessDiagnostic(toolDomain(tool), call, err)
	if !trusted {
		diagnostic = protocolDiagnostic(err)
	}
	attributes := []any{"error_code", redactToken(diagnostic.code, token), "error_message", safeDiagnosticMessage(diagnostic.message, arguments, token)}
	if diagnostic.guiError != nil {
		attributes = append(attributes, "input_may_have_applied", diagnostic.guiError.InputMayHaveApplied)
		switch state := diagnostic.guiError.ClipboardRestore; state {
		case "restored", "failed", "unknown", "skipped_new_owner", "not_requested", "not_used":
			attributes = append(attributes, "clipboard_restore", redactToken(state, token))
		}
	}
	return attributes
}

func protocolDiagnostic(err error) toolDiagnostic {
	switch {
	case errors.Is(err, context.Canceled):
		return toolDiagnostic{code: "cancelled", message: "Tool call cancelled"}
	case errors.Is(err, context.DeadlineExceeded):
		return toolDiagnostic{code: "timeout", message: "Tool call timed out"}
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return toolDiagnostic{code: "invalid_json", message: "Invalid JSON in tool arguments"}
	}
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		return toolDiagnostic{code: "argument_type_mismatch", message: "Tool argument type does not match its declaration"}
	}
	var wire *jsonrpc.Error
	if errors.As(err, &wire) && wire != nil {
		switch wire.Code {
		case jsonrpc.CodeInvalidParams:
			if strings.HasPrefix(wire.Message, "unknown tool ") {
				return toolDiagnostic{code: "unknown_tool", message: "Requested tool is not registered"}
			}
			return toolDiagnostic{code: "invalid_params", message: "Invalid MCP call parameters"}
		case jsonrpc.CodeParseError:
			return toolDiagnostic{code: "invalid_json", message: "Invalid JSON in MCP request"}
		case jsonrpc.CodeInvalidRequest:
			return toolDiagnostic{code: "invalid_request", message: "Invalid MCP request structure"}
		case jsonrpc.CodeMethodNotFound:
			return toolDiagnostic{code: "method_not_found", message: "MCP method is not registered"}
		case jsonrpc.CodeInternalError:
			return toolDiagnostic{code: "internal_error", message: "Internal MCP service error"}
		}
	}
	if err != nil {
		// 锁定 SDK 的 schema 错误有固定前缀，但后续原文可能含实参；只识别类别，不输出原文。
		text := err.Error()
		if strings.HasPrefix(text, `validating "arguments": `) {
			switch {
			case strings.Contains(text, "required: missing properties:"):
				return toolDiagnostic{code: "missing_required_argument", message: "Missing required tool arguments"}
			case strings.Contains(text, ": type:"):
				return toolDiagnostic{code: "argument_type_mismatch", message: "Tool argument type does not match its declaration"}
			case strings.Contains(text, "unmarshaling arguments:"):
				return toolDiagnostic{code: "invalid_argument_json", message: "Tool arguments must be a valid JSON object"}
			default:
				return toolDiagnostic{code: "schema_validation_failed", message: "Tool arguments failed schema validation"}
			}
		}
		if strings.HasPrefix(text, "validating tool output:") {
			return toolDiagnostic{code: "invalid_tool_output", message: "Tool output failed schema validation"}
		}
	}
	return toolDiagnostic{code: "tool_error", message: "Unrecognized tool error; original details are outside the safe logging contract"}
}

func redactToken(text, token string) string {
	if token != "" {
		text = strings.ReplaceAll(text, token, redactionMarker("[redacted]", []string{token}))
		// 普通标记可能与两侧文字拼接出凭据；第二次使用凭据不含的单字符作隔断。
		for _, marker := range []string{"*", "#", "~", "^", "…"} {
			if !strings.Contains(token, marker) {
				return strings.ReplaceAll(text, token, marker)
			}
		}
	}
	return text
}

// 隐藏标记不能重新带入凭据或被屏蔽内容；单字符后备不会等于至少四字符的内容。
func redactionMarker(preferred string, values []string) string {
	for _, candidate := range []string{preferred, "*", "#", "~", "^", "…"} {
		conflict := false
		for _, value := range values {
			if value != "" && strings.Contains(candidate, value) {
				conflict = true
				break
			}
		}
		if !conflict {
			return candidate
		}
	}
	return ""
}

func safeDiagnosticMessage(message string, arguments json.RawMessage, token string) string {
	if len(message) > maxBusinessMessageBytes || !utf8.ValidString(message) {
		message = "Business error details exceed the log limit or have invalid encoding"
	}
	values := diagnosticContentValues(arguments)
	marker := redactionMarker("[content redacted]", append(values, token))
	// 参数只用于额外防止内容回显，不输出到日志；公开枚举和短片段不能遮蔽具体诊断。
	for _, value := range values {
		message = strings.ReplaceAll(message, value, marker)
	}
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, message)
	if strings.TrimSpace(message) == "" {
		message = "Tool did not provide safe error details"
	}
	message = redactToken(message, token)
	truncated := len(message) > maxDiagnosticMessageBytes
	if truncated {
		for len(message) > maxDiagnosticMessageBytes-len("…") {
			_, size := utf8.DecodeLastRuneInString(message)
			message = message[:len(message)-size]
		}
		message += "…"
	}
	// 英文标记、控制字符规范化或截断省略号可能与相邻文字重新拼出敏感内容。
	// 最后再用所有候选均不含的单字符隔断；它不会形成新的凭据或候选值。
	for _, value := range values {
		if !strings.Contains(message, value) {
			continue
		}
		for _, separator := range []string{"*", "#", "~", "^", "…"} {
			if strings.Contains(token, separator) {
				continue
			}
			conflict := false
			for _, candidate := range values {
				if strings.Contains(candidate, separator) {
					conflict = true
					break
				}
			}
			if !conflict {
				for _, candidate := range values {
					message = strings.ReplaceAll(message, candidate, separator)
				}
				if truncated && !strings.HasSuffix(message, "…") {
					// 末尾候选连同省略号被替换时保留截断提示；隔断不在任一候选中，不能重新拼出秘密。
					message += "…"
				}
				return message
			}
		}
		// 极端候选占满全部隔断时保守缩为单字符；候选至少四字符，ASCII Token 不含省略号。
		for _, separator := range []string{"*", "#", "~", "^", "…"} {
			if !strings.Contains(token, separator) {
				return separator
			}
		}
	}
	return message
}

func diagnosticContentValues(arguments json.RawMessage) []string {
	// 只处理有界请求；超长内容不会进入内置模块的固定错误消息。
	if len(arguments) > maxDiagnosticRequestBytes {
		return nil
	}
	var fields map[string]any
	if json.Unmarshal(arguments, &fields) != nil {
		return nil
	}
	values := []string{}
	var collect func(any, int)
	collect = func(value any, depth int) {
		if depth > 8 || len(values) >= 128 {
			return
		}
		switch value := value.(type) {
		case string:
			if utf8.RuneCountInString(value) >= 4 && len(value) <= maxBusinessMessageBytes {
				values = append(values, value)
			}
		case []any:
			for _, item := range value {
				collect(item, depth+1)
			}
		case map[string]any:
			for _, item := range value {
				collect(item, depth+1)
			}
		}
	}
	for _, name := range []string{"text", "path", "dir", "command", "args", "env", "data", "data_base64"} {
		collect(fields[name], 0)
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return values
}
