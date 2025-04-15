package main

import (
	"encoding/json"
	"fmt"
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
func formatMessage(alert WebhookAlert) string {
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("📣 收到告警（总数：%d）\n", len(alert.Alerts)))
	for _, a := range alert.Alerts {
		buf.WriteString("----------------------\n")
		buf.WriteString(fmt.Sprintf("🔔 状态: %s\n", a.Status))
		buf.WriteString(fmt.Sprintf("🚨 名称: %s\n", a.Labels["alertname"]))
		buf.WriteString(fmt.Sprintf("📛 级别: %s\n", a.Labels["severity"]))
		buf.WriteString(fmt.Sprintf("🕒 开始: %s\n", a.StartsAt.Format("2006-01-02 15:04:05")))
		if summary, ok := a.Annotations["summary"]; ok {
			buf.WriteString(fmt.Sprintf("📋 概要: %s\n", summary))
		}
		if desc, ok := a.Annotations["description"]; ok {
			buf.WriteString(fmt.Sprintf("📄 描述: %s\n", desc))
		}
		buf.WriteString(fmt.Sprintf("🔗 链接: %s\n", a.GeneratorURL))
	}
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

	var alert WebhookAlert
	if err := json.NewDecoder(r.Body).Decode(&alert); err != nil {
		http.Error(w, "invalid alert data", http.StatusBadRequest)
		log.Printf("❌ 解码告警数据失败: %v\n", err)
		return
	}

	msg := formatMessage(alert)

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

	log.Printf("✅ 告警已发送到机器人 [%s]，状态：%s\n", robotID, resp.Status)
	w.WriteHeader(http.StatusOK)
}

// main starts the HTTP server.
func main() {
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	http.HandleFunc("/", alertHandler)
	log.Printf("🚀 服务已启动，监听端口 :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("❌ 启动服务失败: %v\n", err)
	}
}
