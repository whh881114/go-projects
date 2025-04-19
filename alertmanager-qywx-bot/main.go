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
// phase: describes the step or operation
// status: the outcome or data associated with that step
type logEntry struct {
	Timestamp string `json:"timestamp"`
	Phase     string `json:"phase"`
	Status    string `json:"status"`
}

// jsonLogWriter formats each log entry into structured JSON and writes to stdout
type jsonLogWriter struct {
	loc *time.Location
}

func (w *jsonLogWriter) Write(p []byte) (n int, err error) {
	// original log message
	msg := strings.TrimSuffix(string(p), "\n")
	// split into phase and status by first ": "
	parts := strings.SplitN(msg, ": ", 2)

	phase := parts[0]
	status := ""
	if len(parts) > 1 {
		status = parts[1]
	}

	// construct structured entry
	timestamp := time.Now().In(w.loc).Format(time.RFC3339)
	entry := logEntry{
		Timestamp: timestamp,
		Phase:     phase,
		Status:    status,
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
	// 在这里，若需记录自定义 phase 和 status，可用格式 "phase: status"
	robotID := strings.TrimPrefix(r.URL.Path, "/")
	if robotID == "" {
		http.Error(w, "robot id missing", http.StatusBadRequest)
		log.Println("错误: 缺少机器人ID")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		log.Printf("阶段: 读取请求体 错误: %v", err)
		return
	}

	var alert AlertmanagerWebhookPayload
	if err := json.Unmarshal(bodyBytes, &alert); err != nil {
		log.Printf("阶段: 解析请求头 结果: %v", r.Header)
		log.Printf("阶段: 打印原始请求体 结果: %s", string(bodyBytes))
		http.Error(w, "invalid alert data", http.StatusBadRequest)
		log.Printf("阶段: 解码告警数据失败 错误: %v", err)
		return
	} else {
		log.Printf("阶段: 解析请求头 结果: %v", r.Header)
		log.Printf("阶段: 打印原始请求体 结果: %s", string(bodyBytes))
	}

	messages := formatMessage(alert)

	payload := map[string]interface{}{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": messages},
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "failed to encode payload", http.StatusInternalServerError)
		log.Printf("阶段: 编码Webhook消息失败 错误: %v", err)
		return
	}

	webhookURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", robotID)
	resp, err := http.Post(webhookURL, "application/json", strings.NewReader(string(payloadJSON)))
	if err != nil {
		http.Error(w, "failed to send to WeChat", http.StatusInternalServerError)
		log.Printf("阶段: 发送到企业微信失败 错误: %v", err)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("阶段: 关闭响应体失败 错误: %v", err)
		}
	}()

	respBody, _ := io.ReadAll(resp.Body)
	// use respBody in log to avoid unused variable error
	log.Printf("服务状态: 启动成功，监控端口为%s，响应内容: %s", resp.Status, string(respBody))
}

func main() {
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	http.HandleFunc("/", alertHandler)
	// 使用自定义 phase:status 格式
	log.Printf("服务状态: 🚀 启动成功，监控端口为%s。", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("阶段: 启动HTTP服务失败 错误: %v", err)
	}
}
