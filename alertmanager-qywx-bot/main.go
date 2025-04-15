package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// WebhookAlert represents the format of a webhook message from AlertManager.
type WebhookAlert struct {
	Alerts []struct {
		Status       string            `json:"status"`
		Labels       map[string]string `json:"labels"`
		Annotations  map[string]string `json:"annotations"`
		StartsAt     time.Time         `json:"startsAt"`
		EndsAt       time.Time         `json:"endsAt"`
		GeneratorURL string            `json:"generatorURL"`
	} `json:"alerts"`
}

// formatMessage formats the Alertmanager alert into a Markdown string suitable for WeChat.
func formatMessage(alert WebhookAlert) []string {
	var messages []string

	for _, a := range alert.Alerts {
		var buf strings.Builder
		if a.Status == "resolved" {
			buf.WriteString(fmt.Sprintf("<font color=\"info\">**【监控告警通知】 ✅✅✅【恢复】✅✅✅**</font>\n"))
		} else {
			buf.WriteString(fmt.Sprintf("<font color=\"warning\">**【监控告警通知】 🔥🔥🔥【故障】🔥🔥🔥**</font>\n"))
		}

		buf.WriteString("----------------------------\n")
		buf.WriteString(fmt.Sprintf("🚨 **状态：** %s\n", a.Status))
		buf.WriteString(fmt.Sprintf("🔔 **名称：** %s\n", a.Labels["alertname"]))
		buf.WriteString(fmt.Sprintf("📛 **级别：** %s\n", a.Labels["severity"]))
		buf.WriteString(fmt.Sprintf("🕒 **开始：** %s\n", a.StartsAt.Format("2006-01-02 15:04:05")))
		if summary, ok := a.Annotations["summary"]; ok {
			buf.WriteString(fmt.Sprintf("📋 **概要**：%s\n", summary))
		}
		if desc, ok := a.Annotations["description"]; ok {
			buf.WriteString(fmt.Sprintf("📄 **描述**：%s\n", desc))
		}
		buf.WriteString(fmt.Sprintf("🔗 链接: [点击访问查询结果](%s)\n", a.GeneratorURL))
		messages = append(messages, buf.String())
	}
	return messages
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
		log.Printf("❌ 无法读取请求体: %v\n", err)
		return
	}

	var alert WebhookAlert
	if err := json.Unmarshal(bodyBytes, &alert); err != nil {
		log.Printf("📦 原始请求体: %s\n", string(bodyBytes))
		log.Printf("📬 请求头: %+v\n", r.Header)
		http.Error(w, "invalid alert data", http.StatusBadRequest)
		log.Printf("❌ 解码告警数据失败: %v\n", err)
		return
	}

	messages := formatMessage(alert)
	for _, msg := range messages {
		payload := map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": msg,
			},
		}

		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			http.Error(w, "failed to encode payload", http.StatusInternalServerError)
			log.Printf("❌ 编码Webhook消息失败: %v\n", err)
			return
		}

		webhookURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", robotID)
		resp, err := http.Post(webhookURL, "application/json", strings.NewReader(string(payloadJSON)))
		if err != nil {
			http.Error(w, "failed to send to WeChat", http.StatusInternalServerError)
			log.Printf("❌ 发送到企业微信失败: %v\n", err)
			return
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("✅ 单条告警已发送到机器人 [%s]，状态：%s，响应内容：%s\n", robotID, resp.Status, string(respBody))
	}
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
		log.Fatalf("❌ 启动服务失败: %v\n", err)
	}
}
