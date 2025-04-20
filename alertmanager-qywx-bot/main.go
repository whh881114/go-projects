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

func init() {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	log.SetFlags(0)
	log.SetOutput(logWriterWithZone(loc))
}

func logWriterWithZone(loc *time.Location) io.Writer {
	return &logWriter{loc: loc}
}

type logWriter struct {
	loc *time.Location
}

func (lw *logWriter) Write(p []byte) (n int, err error) {
	timestamp := time.Now().In(lw.loc).Format("2006-01-02 15:04:05")
	return fmt.Fprintf(os.Stdout, "[%s] %s", timestamp, p)
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
		log.Printf("❌ 无法读取请求体：%v\n", err)
		return
	}

	var alert AlertmanagerWebhookPayload

	// Log raw request body
	log.Printf("📦 告警信息请求体：\n")
	plainLogger.Printf("%s", string(bodyBytes))

	// Attempt to decode JSON body into AlertmanagerWebhookPayload
	if err := json.Unmarshal(bodyBytes, &alert); err != nil {
		log.Printf("❌ 解码告警信息失败：%v\n", err)
		http.Error(w, "invalid alert data", http.StatusBadRequest)
		return
	}

	robotName := path.Base(alert.Receiver)

	messages := formatMessage(alert)

	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": messages,
		},
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "failed to encode payload", http.StatusInternalServerError)
		log.Printf("❌ 编码Webhook消息失败：%v\n", err)
		return
	}

	webhookURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", robotID)
	resp, err := http.Post(webhookURL, "application/json", strings.NewReader(string(payloadJSON)))
	if err != nil {
		http.Error(w, "failed to send to WeChat", http.StatusInternalServerError)
		log.Printf("❌ 发送到企业微信失败：%v\n", err)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("❌ 关闭响应体失败：%v\n", err)
		}
	}()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("✅ 告警信息已发送到机器人：[%s]，状态：%s，响应内容：%s\n", robotName, resp.Status, string(respBody))
}

// main starts the HTTP server.
func main() {
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	http.HandleFunc("/", alertHandler)
	log.Printf("🚀 服务已启动，监听端口：%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("❌ 启动服务失败：%v\n", err)
	}
}
