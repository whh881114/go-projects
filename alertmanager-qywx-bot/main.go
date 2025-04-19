package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// AlertmanagerWebhookPayload represents the format of a webhook message from AlertManager.
type AlertmanagerWebhookPayload struct {
	Receiver          string                   `json:"receiver"`
	Status            string                   `json:"status"`
	Alerts            []map[string]interface{} `json:"alerts"`
	GroupLabels       map[string]interface{}   `json:"groupLabels"`
	CommonLabels      map[string]interface{}   `json:"commonLabels"`
	CommonAnnotations map[string]interface{}   `json:"commonAnnotations"`
	ExternalURL       string                   `json:"externalURL"`
	Version           string                   `json:"version"`
	GroupKey          string                   `json:"groupKey"`
	TruncatedAlerts   int                      `json:"truncatedAlerts"`
}

// logEntry defines the structured JSON log format
// action: describes the step or operation
// result: the outcome or data associated with that step
type logEntry struct {
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"`
	Result    string `json:"result"`
}

// jsonLogWriter formats each log entry into structured JSON and writes to stdout
type jsonLogWriter struct {
	loc *time.Location
}

func (w *jsonLogWriter) Write(p []byte) (n int, err error) {
	// original log message
	msg := strings.TrimSuffix(string(p), "\n")
	// split into action and result
	parts := strings.SplitN(msg, ": ", 2)
	action := parts[0]
	result := ""
	if len(parts) > 1 {
		result = parts[1]
	}

	// construct structured entry
	timestamp := time.Now().In(w.loc).Format(time.RFC3339)
	entry := logEntry{
		Timestamp: timestamp,
		Action:    action,
		Result:    result,
	}
	// serialize to JSON
	b, err := json.Marshal(entry)
	if err != nil {
		return 0, err
	}
	b = append(b, '\n')
	// write to stdout
	n, err = os.Stdout.Write(b)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func init() {
	// set timezone
	loc, _ := time.LoadLocation("Asia/Shanghai")
	// remove default flags
	log.SetFlags(0)
	// redirect log output to structured JSON writer
	log.SetOutput(&jsonLogWriter{loc: loc})
}

// formatMessage formats the AlertmanagerWebhookPayload into a Markdown string suitable for WeChat.
func formatMessage(alert AlertmanagerWebhookPayload) string {
	var buf strings.Builder

	if alert.Status == "resolved" {
		buf.WriteString(fmt.Sprintf("<font color=\"info\">**🌿🌿🌿【监控告警通知】【恢复】🌿🌿🌿**</font>\n"))
	} else {
		buf.WriteString(fmt.Sprintf("<font color=\"warning\">**🔥🔥🔥【监控告警通知】【故障】🔥🔥🔥**</font>\n"))
	}

	buf.WriteString("--------------------------------------------------\n")
	buf.WriteString(fmt.Sprintf("🚨 **状态：** %s\n", alert.Status))
	buf.WriteString(fmt.Sprintf("🔔 **名称：** %s\n", alert.CommonLabels["alertname"]))
	buf.WriteString(fmt.Sprintf("📛 **级别：** %s\n", alert.CommonLabels["severity"]))
	if summary, ok := alert.CommonAnnotations["summary"]; ok {
		buf.WriteString(fmt.Sprintf("📋 **概要：**%s\n", summary))
	}
	if desc, ok := alert.CommonAnnotations["description"]; ok {
		buf.WriteString(fmt.Sprintf("📄 **描述：**%s\n", desc))
	}
	buf.WriteString(fmt.Sprintf("🛠 **故障处理负责人：** %s\n", path.Base(alert.Receiver)))

	return buf.String()
}

// alertHandler handles incoming webhook alerts and forwards them to a WeChat robot webhook URL.
func alertHandler(w http.ResponseWriter, r *http.Request) {
	robotID := strings.TrimPrefix(r.URL.Path, "/")
	if robotID == "" {
		http.Error(w, "robot id missing", http.StatusBadRequest)
		log.Println("❌ 缺少机器人ID")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		log.Printf("❌ 无法读取请求体: %v", err)
		return
	}

	var alert AlertmanagerWebhookPayload
	if err := json.Unmarshal(bodyBytes, &alert); err != nil {
		log.Printf("📬 请求头: %+v", r.Header)
		log.Printf("📦 原始请求体: %s", string(bodyBytes))
		http.Error(w, "invalid alert data", http.StatusBadRequest)
		log.Printf("❌ 解码告警数据失败: %v", err)
		return
	} else {
		log.Printf("📬 请求头: %+v", r.Header)
		log.Printf("📦 原始请求体: %s", string(bodyBytes))
	}

	messages := formatMessage(alert)

	payload := map[string]interface{}{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": messages},
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "failed to encode payload", http.StatusInternalServerError)
		log.Printf("❌ 编码Webhook消息失败: %v", err)
		return
	}

	webhookURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", robotID)
	resp, err := http.Post(webhookURL, "application/json", strings.NewReader(string(payloadJSON)))
	if err != nil {
		http.Error(w, "failed to send to WeChat", http.StatusInternalServerError)
		log.Printf("❌ 发送到企业微信失败: %v", err)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("❌ 关闭响应体失败: %v", err)
		}
	}()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("✅ 单条告警已发送到机器人 [%s]，状态：%s，响应内容：%s", robotID, resp.Status, string(respBody))
}

func main() {
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	http.HandleFunc("/", alertHandler)
	log.Printf("🚀 服务已启动，监听端口：%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("❌ 启动服务失败: %v", err)
	}
}
