package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type RequestBody struct {
	Server    string   `json:"server"`
	Token     string   `json:"token"`
	App       string   `json:"app"`
	Resources []string `json:"resources"`
	Timeout   int      `json:"timeout"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type SuccessResponse struct {
	Message string `json:"message"`
}

func main() {
	http.HandleFunc("/", restartHandler)
	log.Println("启动服务，监听 :8080")
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

	for _, p := range reqBody.Resources {
		if err := doRestart(reqBody.Server, reqBody.Token, reqBody.App, p, timeout); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("资源 %s 重启失败: %v", p, err))
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(SuccessResponse{Message: "所有资源重启已触发"}); err != nil {
		log.Printf("响应写入失败: %v", err)
		http.Error(w, fmt.Sprintf("写入响应失败: %v", err), http.StatusInternalServerError)
	}
}

func validateReq(req RequestBody) error {
	if req.Server == "" || req.Token == "" || req.App == "" || len(req.Resources) == 0 {
		return fmt.Errorf("server, token, app 及 resources 均为必填项")
	}
	return nil
}

func doRestart(server, token, app, resource string, timeout int) error {
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
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("关闭响应体失败: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	return nil
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(ErrorResponse{Error: msg}); err != nil {
		log.Printf("写入错误响应失败: %v", err)
	}
}

// RBAC 配置示例：
// p, role:restart-only, applications, action/*, *, allow
// g, syncbot, role:restart-only
