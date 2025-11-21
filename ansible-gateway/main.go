package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
	yaml "gopkg.in/yaml.v3"
)

type ServerCfg struct {
	Addr         string
	ReadTimeout  string
	WriteTimeout string
	IdleTimeout  string
}

type RedisCfg struct {
	Addr     string
	Password string
	DB       int
}

type AnsibleCfg struct {
	Dir  string
	Log  string
	User string
}

type Config struct {
	Server  ServerCfg
	Redis   RedisCfg
	Ansible AnsibleCfg
}

// 请求与校验
var (
	idRe       = regexp.MustCompile(`^[a-zA-Z0-9]+-[a-zA-Z0-9]+(?:-[a-zA-Z0-9]+)*$`)
	hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9]+-[a-zA-Z0-9]+(?:-[a-zA-Z0-9]+)*-\d{3}$`)
	ipRe       = regexp.MustCompile(`^(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)$`)
)

type HostReq struct {
	ID       string `json:"ID"`
	Hostname string `json:"Hostname"`
	IP       string `json:"IP"`
}

type App struct {
	cfg Config
	rdb *redis.Client
}

// 全局日志文件状态（用于 SIGUSR1 轮转）
var (
	logFile     *os.File
	logFilePath string
)

// 主程序
func main() {
	cfgPath := flag.String("config", "./config.yaml", "path to config file")
	logPath := flag.String("logfile", "", "path to log file (empty=stderr)")
	pidPath := flag.String("pidfile", "", "path to pid file")
	flag.Parse()

	// 日志初始化：如果指定了 logfile，就把 log 输出导向文件
	if err := setupLog(*logPath); err != nil {
		log.Fatalf("setup log: %v", err)
	}

	// 写 pidfile（nginx 同款风格）
	if *pidPath != "" {
		if err := os.WriteFile(*pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
			log.Fatalf("write pidfile: %v", err)
		}
	}

	// 监听 SIGUSR1：收到后重开日志文件，配合 logrotate 的 postrotate
	if *logPath != "" {
		go handleLogReopen()
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	app := &App{cfg: cfg, rdb: rdb}

	// gin 初始化
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// v1 host API
	v1Host := r.Group("/v1/host")
	{
		v1Host.POST("/register", app.registerHost)
		v1Host.POST("/unregister", app.unregisterHost)
	}

	server := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      r,
		ReadTimeout:  mustDur(cfg.Server.ReadTimeout, 10*time.Second),
		WriteTimeout: mustDur(cfg.Server.WriteTimeout, 0),
		IdleTimeout:  mustDur(cfg.Server.IdleTimeout, 120*time.Second),
	}

	log.Printf("ansible-gateway listening on %s", cfg.Server.Addr)
	log.Fatal(server.ListenAndServe())
}

// 初始化 / 重新打开日志文件
func setupLog(path string) error {
	// 空路径：保留默认行为（stderr），方便开发调试
	if path == "" {
		logFilePath = ""
		logFile = nil
		log.SetOutput(os.Stderr)
		return nil
	}

	// 确保目录存在
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir log dir: %w", err)
		}
	}

	// 打开新文件
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	// 关闭旧文件（如果有）
	if logFile != nil {
		_ = logFile.Close()
	}

	logFile = f
	logFilePath = path
	log.SetOutput(f)
	return nil
}

// 处理 SIGUSR1：重开日志文件，配合 logrotate
func handleLogReopen() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	for range ch {
		if logFilePath == "" {
			continue
		}
		if err := setupLog(logFilePath); err != nil {
			// 这里不能用 log.Printf（可能已经坏了），直接写 stderr
			fmt.Fprintf(os.Stderr, "reopen log file on SIGUSR1 failed: %v\n", err)
		} else {
			log.Printf("log file reopened on SIGUSR1")
		}
	}
}

// 配置加载
func loadConfig(path string) (Config, error) {
	// 1. 读文件（只负责把 bytes 读出来）
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	// 2. YAML 解析（负责“看得懂”缩进、冒号、列表等语法）
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config yaml: %w", err)
	}

	return cfg, nil
}

// 校验请求体
func validate(req HostReq) error {
	if req.ID == "" || req.Hostname == "" || req.IP == "" {
		return errors.New("missing id/hostname/ip")
	}
	if !idRe.MatchString(req.ID) {
		return fmt.Errorf("invalid id: %s", req.ID)
	}
	if !hostnameRe.MatchString(req.Hostname) {
		return fmt.Errorf("invalid hostname: %s", req.Hostname)
	}
	if !ipRe.MatchString(req.IP) {
		return fmt.Errorf("invalid ip: %s", req.IP)
	}
	return nil
}

// 时间函数
func mustDur(s string, d time.Duration) time.Duration {
	if s == "" || s == "0" {
		return d
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return d
	}
	return v
}

// ansible playbook相关函数
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func findPlaybookFile(dir, base string) (string, bool, error) {
	ymlPath := filepath.Join(dir, base+".yml")
	yamlPath := filepath.Join(dir, base+".yaml")

	ymlExists := fileExists(ymlPath)
	yamlExists := fileExists(yamlPath)

	if ymlExists && yamlExists {
		return "", false, fmt.Errorf("ambiguous playbook: both %s and %s exist", ymlPath, yamlPath)
	}
	if ymlExists {
		return ymlPath, true, nil
	}
	if yamlExists {
		return yamlPath, true, nil
	}
	return "", false, fmt.Errorf("no playbook found: %s.{yml|yaml}", base)
}

func selectPlaybook(dir, hostgroup string) (playbook, warn string, err error) {
	def, defExists, _ := findPlaybookFile(dir, "default")
	hg, hgExists, _ := findPlaybookFile(dir, hostgroup)

	switch {
	case defExists && !hgExists:
		return def, fmt.Sprintf("hostgroup playbook missing: %s.{yml|yaml}; fallback to default", hostgroup), nil

	case !defExists && hgExists:
		return hg, "", nil

	case defExists && hgExists:
		return hg, "both default and hostgroup exist; prefer hostgroup", nil

	default:
		return "", "", fmt.Errorf("neither default nor hostgroup playbook exists under %s (tried .yml/.yaml)", dir)
	}
}

// 主机注册逻辑
func (a *App) registerHost(c *gin.Context) {
	// 只允许 POST（路由已经是 POST，这里再兜一层）
	if c.Request.Method != http.MethodPost {
		c.String(http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req HostReq
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20)).Decode(&req); err != nil {
		c.String(http.StatusBadRequest, "invalid json")
		return
	}
	if err := validate(req); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	// 计算 hostgroup（去掉最后的 -NNN），用于ansible的hostgroup
	parts := strings.Split(req.Hostname, "-")
	hostgroup := strings.Join(parts[:len(parts)-1], "-")

	// 流式输出（避免一次性缓冲导致代理读超时）
	w := c.Writer
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher, ok := w.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// 直接使用标准库 log，带时间戳；logf 只是包装一下做 flush
	logger := log.New(w, "", log.LstdFlags|log.Lmicroseconds)
	logf := func(format string, args ...any) {
		logger.Printf(format, args...)
		flusher.Flush()
	}

	ctx := c.Request.Context()

	// Redis 锁
	lockKey := "LOCK__" + req.Hostname
	val := req.ID + "__" + req.IP
	logf("[INFO] trying to register: %s", lockKey)

	okSet, err := a.rdb.HSetNX(ctx, lockKey, "id__ip", val).Result()

	if err != nil {
		http.Error(w, "redis error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if okSet {
		logf("[INFO] registered")
	} else {
		stored, _ := a.rdb.HGet(ctx, lockKey, "id__ip").Result()
		// 冲突：不同的 ID/IP 抢同一个 hostname
		if stored != val {
			logf("[ERROR] registration conflict: stored=%q, incoming=%q", stored, val)
			http.Error(
				w,
				fmt.Sprintf("[CONFLICT] already registered by %q, incoming=%q", stored, val),
				http.StatusConflict,
			)
			return
		}
		// 幂等：相同的请求再次进来，放行
		logf("[WARN] already registered (idempotent), stored=%q", stored)
	}

	// 选 playbook
	playbook, warn, err := selectPlaybook(a.cfg.Ansible.Dir, hostgroup)
	if warn != "" {
		logf("[WARN] %s", warn)
	}
	if err != nil {
		http.Error(w, "playbook select error: "+err.Error(), http.StatusNotFound)
		return
	}
	logf("[INFO] use playbook: %s", playbook)

	// 写 inventory 文件
	if err := os.MkdirAll(a.cfg.Ansible.Log, 0o755); err != nil {
		http.Error(w, "mkdir log_dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	invBase := fmt.Sprintf("%s__%s__%s.txt", req.ID, req.Hostname, req.IP)
	invPath := filepath.Join(a.cfg.Ansible.Log, invBase)
	if err := os.WriteFile(invPath, []byte("["+hostgroup+"]\n"+req.IP+"\n"), 0o644); err != nil {
		http.Error(w, "write inventory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logf("[INFO] inventory written: %s", invPath)

	// 步骤 1：设置主机名（通过 ansible 模块 shell）
	hostnameCmd := fmt.Sprintf(
		"ansible -u %s %s -i %s -m shell -a 'hostnamectl set-hostname %s'",
		a.cfg.Ansible.User, req.IP, invPath, req.Hostname,
	)
	// timeout=0，等ansible命令执行完或执行过程中报错
	if err := a.runAndStream(ctx, hostnameCmd, w, logf); err != nil {
		http.Error(w, "hostname step failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 步骤 2：执行 playbook 并 tee 到日志
	logFile := strings.TrimSuffix(invPath, ".txt") +
		"__" + time.Now().Format("2006-01-02_15:04:05.000000") + ".log"

	playbookCmd := fmt.Sprintf(
		"cd %s && ansible-playbook %s -i %s -e hosts=%s 2>&1 | tee %s",
		a.cfg.Ansible.Dir, playbook, invPath, hostgroup, logFile,
	)
	if err := a.runAndStream(ctx, playbookCmd, w, logf); err != nil {
		http.Error(w, "playbook step failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logf("[INFO] initialize host done. log=%s", logFile)
}

// 解除注册：清理 Redis 键，便于后续重新注册（迁移/重装主机）
func (a *App) unregisterHost(c *gin.Context) {
	if c.Request.Method != http.MethodPost {
		c.String(http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req HostReq
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20)).Decode(&req); err != nil {
		c.String(http.StatusBadRequest, "invalid json")
		return
	}
	// 复用和 register 一样的校验逻辑：ID / Hostname / IP 都必填且格式正确
	if err := validate(req); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	lockKey := "LOCK__" + req.Hostname
	ctx := c.Request.Context()

	// 必须和 Redis 里存的一致才允许删除
	incoming := req.ID + "__" + req.IP
	stored, _ := a.rdb.HGet(ctx, lockKey, "id__ip").Result()
	if stored != incoming {
		c.String(http.StatusPreconditionFailed, "mismatch: stored=%q incoming=%q", stored, incoming)
		return
	}

	_, _ = a.rdb.Del(ctx, lockKey).Result()
	c.JSON(http.StatusOK, map[string]any{
		"ok":      true,
		"deleted": lockKey,
	})
}

// 封闭执行shell命令函数
func (a *App) runAndStream(ctx context.Context, shellCmd string, w http.ResponseWriter, logf func(string, ...any)) error {
	logf("[INFO] run: %s", shellCmd)

	c := ctx
	cmd := exec.CommandContext(c, "/bin/bash", "-lc", shellCmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// 合并两路输出并保持行级刷新
	merge := func(r io.Reader, prefix string) error {
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
		for s.Scan() {
			fmt.Fprintf(w, "%s %s\n", prefix, s.Text())
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		return s.Err()
	}

	// 并发读取 stdout/stderr
	done := make(chan error, 2)
	go func() { done <- merge(stdout, "[OUT]") }()
	go func() { done <- merge(stderr, "[ERR]") }()

	for i := 0; i < 2; i++ {
		if e := <-done; e != nil {
			logf("[WARN] stream: %v", e)
		}
	}

	err = cmd.Wait()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("command timeout")
		}
		return err
	}

	return nil
}
