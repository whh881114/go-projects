package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// RequestBody 定义 POST 请求体结构
// example:
//
//	{
//	  "server": "https://argocd.example.com",
//	  "token": "<ARGOCD_TOKEN>",
//	  "app": "my-app",
//	  "resources": [
//	    "apps:Deployment:default/my-deploy",
//	    "argoproj.io:Rollout:prod-rollout"
//	  ],
//	  "timeout": 10
//	}
type RequestBody struct {
	Server    string   `json:"server"`
	Token     string   `json:"token"`
	App       string   `json:"app"`
	Resources []string `json:"resources"`
	Timeout   int      `json:"timeout"` // 可选，默认10秒
}

// ErrorResponse 用于返回错误信息
type ErrorResponse struct {
	Error string `json:"error"`
}

// SuccessResponse 用于返回成功信息
type SuccessResponse struct {
	Message string `json:"message"`
}

func main() {
	http.HandleFunc("/restart", restartHandler)
	log.Println("启动 Argo CD 重启服务，监听 :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

func restartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 方法", http.StatusMethodNotAllowed)
		return
	}

	var reqBody RequestBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("解析请求体失败: %v", err))
		return
	}
	if err := validateReq(reqBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	timeout := reqBody.Timeout
	if timeout <= 0 {
		timeout = 10
	}

	// 依次重启资源
	for _, p := range reqBody.Resources {
		if err := doRestart(reqBody.Server, reqBody.Token, reqBody.App, p, timeout); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("资源 %s 重启失败: %v", p, err))
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SuccessResponse{Message: "所有资源重启已触发"})
}

func validateReq(req RequestBody) error {
	if req.Server == "" || req.Token == "" || req.App == "" || len(req.Resources) == 0 {
		return fmt.Errorf("server, token, app 及 resources 均为必填项")
	}
	return nil
}

func doRestart(server, token, app, resource string, timeout int) error {
	// resource 格式: <group>:<kind>:<namespace>/<name>
	parts := strings.SplitN(resource, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("格式错误: %s", resource)
	}
	gk := strings.Split(parts[0], ":")
	if len(gk) != 3 {
		return fmt.Errorf("group:kind:namespace 格式错误: %s", parts[0])
	}
	group, kind, namespace, name := gk[0], gk[1], gk[2], parts[1]

	url := fmt.Sprintf(
		"%s/api/v1/applications/%s/resource/actions?group=%s&kind=%s&namespace=%s&name=%s&action=restart",
		server, app, group, kind, namespace, name,
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// writeError 统一写错误响应
func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}
