package notification

import (
	"encoding/base64"
	"fmt"
	"html"
	"mime"
	"strings"
	"time"
)

const (
	messageBrand       = "iCloud HME"
	messageDisplayName = "iCloud HME 通知"
	messageTimeLayout  = "2006-01-02 15:04:05 -0700"
	messageFooter      = "本邮件由本地部署的 iCloud HME 服务自动发送，请勿直接回复。"
	messagePrivacyNote = "出于隐私保护，正文不包含别名地址、Cookie、App 专用密码或邮件内容。"
	messageTextRule    = "────────────────────────────────────────"
)

// Message is a rendered notification email. Text is always present and HTML is
// delivered as a multipart/alternative part for clients that render it.
type Message struct {
	Subject string
	Text    string
	HTML    string
	Date    time.Time
}

// messageField is one label/value row inside a section.
type messageField struct {
	Label string
	Value string
}

// messageSection groups related rows so the reader can scan the summary,
// the counters, and the follow-up notes separately.
type messageSection struct {
	Title  string
	Fields []messageField
}

// renderEvent turns a redacted event into a subject line, a structured plain
// text body, and an HTML alternative.
func renderEvent(event Event, at time.Time) Message {
	kind := eventKind(event)
	status := eventStatus(event)
	trigger := statusValue(event.Trigger)

	summary := messageSection{Title: "事件概要", Fields: []messageField{
		{Label: "事件类型", Value: labelWithCode(kindLabel(kind), kind)},
		{Label: "执行状态", Value: labelWithCode(statusLabel(status), status)},
		{Label: "触发方式", Value: labelWithCode(triggerLabel(trigger), trigger)},
	}}
	if account := oneLine(event.AccountID); account != "" {
		summary.Fields = append(summary.Fields, messageField{Label: "关联账户", Value: redactAccountID(account)})
	}
	summary.Fields = append(summary.Fields, messageField{Label: "发生时间", Value: at.Format(messageTimeLayout)})
	sections := []messageSection{summary}

	if showsCounters(kind, event) {
		sections = append(sections, messageSection{Title: "创建统计", Fields: []messageField{
			{Label: "计划创建", Value: fmt.Sprintf("%d 个", event.Requested)},
			{Label: "成功创建", Value: fmt.Sprintf("%d 个", event.Created)},
			{Label: "创建失败", Value: fmt.Sprintf("%d 个", event.Failed)},
			{Label: "完成状态", Value: completeLabel(event.Complete)},
		}})
	}

	var notes []messageField
	if reason := oneLine(event.PauseReason); reason != "" {
		notes = append(notes, messageField{Label: "暂停原因", Value: labelWithCode(pauseReasonLabel(reason), reason)})
	}
	if errorSummary := oneLine(event.Error); errorSummary != "" {
		notes = append(notes, messageField{Label: "错误摘要", Value: errorSummary})
	}
	if action := recommendedAction(kind, status); action != "" {
		notes = append(notes, messageField{Label: "建议操作", Value: action})
	}
	if len(notes) > 0 {
		sections = append(sections, messageSection{Title: "补充信息", Fields: notes})
	}

	subject := eventSubject(kind, status, event)
	headline := eventHeadline(kind, status, event)
	return Message{
		Subject: subject,
		Text:    renderText(headline, sections),
		HTML:    renderHTML(subject, headline, status, sections),
		Date:    at,
	}
}

// renderTestMessage confirms the saved 163-to-QQ channel works and restates
// what a real notification will contain.
func renderTestMessage(config Config, at time.Time) Message {
	sections := []messageSection{
		{Title: "通道信息", Fields: []messageField{
			{Label: "邮件服务", Value: "163 邮箱 (SMTP over SSL)"},
			{Label: "服务地址", Value: fmt.Sprintf("%s:%d", SMTPHost, SMTPPort)},
			{Label: "发件邮箱", Value: oneLine(config.SenderEmail)},
			{Label: "收件邮箱", Value: oneLine(config.RecipientEmail)},
			{Label: "发送时间", Value: at.Format(messageTimeLayout)},
		}},
		{Title: "后续通知", Fields: []messageField{
			{Label: "通知事件", Value: "别名自动化执行结果、规则自动暂停、iCloud 会话失效"},
			{Label: "投递方式", Value: "异步队列发送，失败最多重试 3 次，不会阻塞别名任务"},
			{Label: "正文内容", Value: "仅包含脱敏账户标识、事件类型、状态、数量与错误摘要"},
		}},
	}
	const headline = "邮件通知渠道测试成功，当前 163 邮箱配置已生效。"
	subject := fmt.Sprintf("[%s] 邮件通知测试 · 配置已生效", messageBrand)
	return Message{
		Subject: subject,
		Text:    renderText(headline, sections),
		HTML:    renderHTML(subject, headline, "test", sections),
		Date:    at,
	}
}

func renderText(headline string, sections []messageSection) string {
	body := strings.Builder{}
	body.WriteString(messageBrand + " 通知\r\n")
	body.WriteString(messageTextRule + "\r\n")
	body.WriteString(headline + "\r\n")
	for _, section := range sections {
		body.WriteString("\r\n【" + section.Title + "】\r\n")
		for _, field := range section.Fields {
			fmt.Fprintf(&body, "%s：%s\r\n", field.Label, field.Value)
		}
	}
	body.WriteString("\r\n" + messageTextRule + "\r\n")
	body.WriteString(messageFooter + "\r\n")
	body.WriteString(messagePrivacyNote + "\r\n")
	return body.String()
}

// renderHTML uses table layout and inline styles because mail clients strip
// stylesheets and collapse modern layout primitives.
func renderHTML(subject, headline, status string, sections []messageSection) string {
	body := strings.Builder{}
	body.WriteString(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8">`)
	body.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	fmt.Fprintf(&body, "<title>%s</title></head>", html.EscapeString(subject))
	body.WriteString(`<body style="margin:0;padding:24px 12px;background-color:#f1f5f9;` + htmlFontStack + `color:#0f172a;">`)
	body.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="max-width:600px;margin:0 auto;background-color:#ffffff;border:1px solid #e2e8f0;border-radius:10px;">`)

	body.WriteString(`<tr><td style="padding:18px 24px;border-bottom:1px solid #e2e8f0;">`)
	fmt.Fprintf(&body, `<div style="font-size:16px;font-weight:600;letter-spacing:.01em;">%s</div>`, html.EscapeString(messageDisplayName))
	body.WriteString(`<div style="margin-top:4px;font-size:12px;color:#64748b;">本地自动化服务 · 自动发送</div></td></tr>`)

	body.WriteString(`<tr><td style="padding:20px 24px 0;">`)
	fmt.Fprintf(
		&body,
		`<span style="display:inline-block;padding:3px 10px;border-radius:999px;font-size:12px;font-weight:600;color:#ffffff;background-color:%s;">%s</span>`,
		statusColor(status),
		html.EscapeString(statusLabel(status)),
	)
	fmt.Fprintf(&body, `<p style="margin:12px 0 0;font-size:14px;line-height:1.7;">%s</p></td></tr>`, html.EscapeString(headline))

	for _, section := range sections {
		body.WriteString(`<tr><td style="padding:18px 24px 0;">`)
		fmt.Fprintf(
			&body,
			`<div style="font-size:12px;font-weight:600;color:#475569;letter-spacing:.04em;">%s</div>`,
			html.EscapeString(section.Title),
		)
		body.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="margin-top:8px;border-collapse:collapse;font-size:13px;line-height:1.6;">`)
		for _, field := range section.Fields {
			body.WriteString(`<tr>`)
			fmt.Fprintf(
				&body,
				`<td style="padding:7px 12px 7px 0;width:76px;color:#64748b;border-bottom:1px solid #f1f5f9;white-space:nowrap;vertical-align:top;">%s</td>`,
				html.EscapeString(field.Label),
			)
			fmt.Fprintf(
				&body,
				`<td style="padding:7px 0;color:#0f172a;border-bottom:1px solid #f1f5f9;word-break:break-word;">%s</td>`,
				html.EscapeString(field.Value),
			)
			body.WriteString(`</tr>`)
		}
		body.WriteString(`</table></td></tr>`)
	}

	body.WriteString(`<tr><td style="padding:18px 24px 20px;"><div style="padding-top:16px;border-top:1px solid #e2e8f0;font-size:12px;line-height:1.7;color:#64748b;">`)
	fmt.Fprintf(&body, `%s<br>%s`, html.EscapeString(messageFooter), html.EscapeString(messagePrivacyNote))
	body.WriteString(`</div></td></tr></table></body></html>`)
	return body.String()
}

const htmlFontStack = `font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif;`

func eventKind(event Event) string {
	if kind := oneLine(event.Kind); kind != "" {
		return kind
	}
	return "alias_automation"
}

func eventStatus(event Event) string {
	if status := oneLine(event.Status); status != "" {
		return status
	}
	return "unknown"
}

// eventSubject keeps the inbox list scannable: what happened, how it ended,
// how many aliases were added, and which account it belongs to.
func eventSubject(kind, status string, event Event) string {
	subject := fmt.Sprintf("[%s] %s · %s", messageBrand, kindLabel(kind), statusLabel(status))
	if showsCounters(kind, event) && event.Requested > 0 {
		subject += fmt.Sprintf(" · 新增 %d/%d", event.Created, event.Requested)
	}
	if account := oneLine(event.AccountID); account != "" {
		subject += " · 账户 " + redactAccountID(account)
	}
	return subject
}

func eventHeadline(kind, status string, event Event) string {
	switch kind {
	case "session_expired":
		return "iCloud 会话已失效，该账户的别名自动化任务已无法继续执行。"
	case "alias_automation_paused":
		if event.Created > 0 {
			return fmt.Sprintf("别名自动化规则已自动暂停，本次执行新增 %d 个别名。", event.Created)
		}
		return "别名自动化规则已自动暂停，本次执行未新增别名。"
	case "alias_automation":
		switch status {
		case "success":
			return fmt.Sprintf("别名自动化执行成功，本次新增 %d 个别名。", event.Created)
		case "partial":
			return fmt.Sprintf("别名自动化部分完成，成功 %d 个、失败 %d 个。", event.Created, event.Failed)
		case "skipped":
			return "本次别名自动化未执行，可能未满足执行条件或已达当日创建上限。"
		case "error":
			return "别名自动化执行失败，本次未新增别名。"
		}
		return fmt.Sprintf("别名自动化执行结束，当前状态为%s。", statusLabel(status))
	}
	return fmt.Sprintf("%s事件已触发，当前状态为%s。", kindLabel(kind), statusLabel(status))
}

// recommendedAction is only filled in when the reader has something to do.
func recommendedAction(kind, status string) string {
	switch kind {
	case "session_expired":
		return "请在控制台重新登录该 iCloud 账户，恢复前相关自动化任务会持续跳过。"
	case "alias_automation_paused":
		return "请在控制台核对自动化规则，处理后重新启用即可继续执行。"
	}
	switch status {
	case "error":
		return "请检查账户登录状态与网络代理后重试，连续失败达到上限会自动暂停规则。"
	case "partial":
		return "部分别名创建失败，可在控制台的别名创建历史中查看明细后重试。"
	}
	return ""
}

// showsCounters hides the counter block for events where the numbers carry no
// meaning, such as an expired iCloud session.
func showsCounters(kind string, event Event) bool {
	if strings.HasPrefix(kind, "alias_automation") {
		return true
	}
	return event.Requested > 0 || event.Created > 0 || event.Failed > 0
}

func labelWithCode(label, code string) string {
	code = oneLine(code)
	if label == "" || label == code {
		return code
	}
	if code == "" {
		return label
	}
	return fmt.Sprintf("%s (%s)", label, code)
}

func kindLabel(kind string) string {
	switch kind {
	case "alias_automation":
		return "别名自动化执行"
	case "alias_automation_paused":
		return "别名自动化暂停"
	case "session_expired":
		return "iCloud 会话失效"
	case "webhook_test", "email_test":
		return "通知渠道测试"
	default:
		return oneLine(kind)
	}
}

func statusLabel(status string) string {
	switch status {
	case "success":
		return "成功"
	case "partial":
		return "部分成功"
	case "skipped":
		return "已跳过"
	case "error":
		return "失败"
	case "test":
		return "测试"
	case "unknown":
		return "未知"
	default:
		return oneLine(status)
	}
}

func triggerLabel(trigger string) string {
	switch trigger {
	case "manual":
		return "手动触发"
	case "scheduled":
		return "定时触发"
	case "system":
		return "系统触发"
	case "unknown":
		return "未知"
	default:
		return oneLine(trigger)
	}
}

func pauseReasonLabel(reason string) string {
	switch reason {
	case "target_reached":
		return "已达到累计创建目标"
	case "alias_limit":
		return "已达到别名总量上限"
	case "failure_limit":
		return "连续失败次数达到上限"
	case "manual":
		return "手动暂停"
	default:
		return oneLine(reason)
	}
}

func completeLabel(complete bool) string {
	if complete {
		return "已完成"
	}
	return "未完成"
}

func statusColor(status string) string {
	switch status {
	case "success":
		return "#15803d"
	case "partial", "skipped":
		return "#b45309"
	case "error":
		return "#b91c1c"
	case "test":
		return "#1d4ed8"
	default:
		return "#334155"
	}
}

// buildMIMEMessage renders a multipart/alternative message. Both parts are
// base64 encoded so UTF-8 bodies survive servers that do not offer 8BITMIME.
// Headers are folded through oneLine so no rendered value can inject a header.
func buildMIMEMessage(config Config, message Message) (string, error) {
	token, err := randomDeliveryID()
	if err != nil {
		return "", fmt.Errorf("create message identifier: %w", err)
	}
	date := message.Date
	if date.IsZero() {
		date = time.Now()
	}
	boundary := "=_hme_" + token
	encodeHeader := func(value string) string {
		return mime.QEncoding.Encode("UTF-8", oneLine(value))
	}

	raw := strings.Builder{}
	fmt.Fprintf(&raw, "From: %s <%s>\r\n", encodeHeader(messageDisplayName), oneLine(config.SenderEmail))
	fmt.Fprintf(&raw, "To: %s\r\n", oneLine(config.RecipientEmail))
	fmt.Fprintf(&raw, "Subject: %s\r\n", encodeHeader(message.Subject))
	fmt.Fprintf(&raw, "Date: %s\r\n", date.Format(time.RFC1123Z))
	fmt.Fprintf(&raw, "Message-ID: <%s@%s>\r\n", token, senderDomain(config.SenderEmail))
	raw.WriteString("Auto-Submitted: auto-generated\r\n")
	raw.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&raw, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary)
	// The preamble stays ASCII so it survives servers without 8BITMIME; the
	// readable parts below are base64 encoded for the same reason.
	raw.WriteString("This is a multi-part message in MIME format.\r\n\r\n")

	fmt.Fprintf(&raw, "--%s\r\n", boundary)
	raw.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	raw.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	raw.WriteString(encodeBase64Lines(message.Text))

	if strings.TrimSpace(message.HTML) != "" {
		fmt.Fprintf(&raw, "--%s\r\n", boundary)
		raw.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		raw.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		raw.WriteString(encodeBase64Lines(message.HTML))
	}
	fmt.Fprintf(&raw, "--%s--\r\n", boundary)
	return raw.String(), nil
}

func senderDomain(sender string) string {
	if _, domain, ok := strings.Cut(strings.TrimSpace(sender), "@"); ok && domain != "" {
		return domain
	}
	return SMTPHost
}

func encodeBase64Lines(value string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\n", "\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(normalized))
	const lineLength = 76
	lines := strings.Builder{}
	for start := 0; start < len(encoded); start += lineLength {
		lines.WriteString(encoded[start:min(start+lineLength, len(encoded))])
		lines.WriteString("\r\n")
	}
	return lines.String()
}
