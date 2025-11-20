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
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
	yaml "gopkg.in/yaml.v3"
)

// -------------------- 配置结构 --------------------

type ServerCfg struct {
	Addr         string `yaml:"addr"`
	ReadTimeout  string `yaml:"read_timeout"`
	WriteTimeout string `yaml:"write_timeout"`
	IdleTimeout  string `yaml:"idle_timeout"`
}

type RedisCfg struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type AnsibleCfg struct {
	PlaybookRoot string `yaml:"playbook_root"`
	LogDir       string `yaml:"log_dir"`
	User         string `yaml:"user"`
}

type ExecCfg struct {
	HostnameTimeout string   `yaml:"hostname_timeout"`
	PlaybookTimeout string   `yaml:"playbook_timeout"`
	OverallTimeout  string   `yaml:"overall_timeout"`
	Env             []string `yaml:"env"`
}

type Config struct {
	Server  ServerCfg  `yaml:"server"`
	Redis   RedisCfg   `yaml:"redis"`
	Ansible AnsibleCfg `yaml:"ansible"`
	Exec    ExecCfg    `yaml:"exec"`
}

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config yaml: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.Addr == "" {
		return fmt.Errorf("server.addr is required")
	}
	if c.Redis.Addr == "" {
		return fmt.Errorf("redis.addr is required")
	}
	if c.Ansible.PlaybookRoot == "" {
		return fmt.Errorf("ansible.playbook_root is required")
	}
	if c.Ansible.LogDir == "" {
		return fmt.Errorf("ansible.log_dir is required")
	}
	if c.Ansible.User == "" {
		return fmt.Errorf("ansible.user is required")
	}
	return nil
}

// -------------------- 校验用正则 --------------------

var (
	idRe       = regexp.MustCompile(`^[a-zA-Z0-9]+-[a-zA-Z0-9]+(?:-[a-zA-Z0-9]+)*$`)
	hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9]+-[a-zA-Z0-9]+(?:-[a-zA-Z0-9]+)*-\d{3}$`)
	ipRe       = regexp.MustCompile(`^(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|[0-1]\d{2}|[1-9]?\d)$`)
)

// -------------------- 请求结构 --------------------

type HostReq struct {
	ID       string `json:"ID"`
	Hostname string `json:"Hostname"`
	IP       string `json:"IP"`
}

func validateHostReq(req HostReq) error {
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

// -------------------- App --------------------

type App struct {
	cfg Config
	rdb *redis.Client
}

func mustDur(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	if s == "0" || s == "0s" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

// -------------------- 入口 main --------------------

func main() {
	cfgPath := flag.String("config", "./config.yaml", "path to config file")
	flag.Parse()

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	app := &App{cfg: cfg, rdb: rdb}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// 健康检查
	r.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// v1 host API
	v1Host := r.Group("/v1/host")
	{
		v1Host.POST("/registry", app.handleRegistry)
		v1Host.POST("/unregistry", app.handleUnregistry)
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

// -------------------- 处理器：注册 --------------------

func (a *App) handleRegistry(c *gin.Context) {
	if c.Request.Method != http.MethodPost {
		c.String(http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req HostReq
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20)).Decode(&req); err != nil {
		c.String(http.StatusBadRequest, "invalid json")
		return
	}
	if err := validateHostReq(req); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	// hostgroup = hostname 去掉最后一段 -NNN
	parts := strings.Split(req.Hostname, "-")
	hostgroup := strings.Join(parts[:len(parts)-1], "-")

	w := c.Writer
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	flusher, ok := w.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// OverallTimeout 包一层
	ctx := c.Request.Context()
	if ov := a.cfg.Exec.OverallTimeout; ov != "" && ov != "0s" && ov != "0" {
		if d, err := time.ParseDuration(ov); err == nil && d > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
	}

	logf := func(format string, args ...any) {
		t := time.Now().Format("2006-01-02_15:04:05.000000")
		fmt.Fprintf(w, "%s %s\n", t, fmt.Sprintf(format, args...))
		flusher.Flush()
	}

	// Redis 锁
	lockKey := "LOCK__" + req.Hostname
	val := req.ID + "__" + req.IP

	logf("[INFO] trying to register: %s", lockKey)
	okSet, err := a.rdb.HSetNX(ctx, lockKey, "id__ip", val).Result()
	if err != nil {
		logf("[ERROR] redis HSetNX failed: %v", err)
		http.Error(w, "redis error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if okSet {
		logf("[INFO] registered")
	} else {
		stored, _ := a.rdb.HGet(ctx, lockKey, "id__ip").Result()
		if stored == val {
			logf("[WARN] already registered (idempotent), stored=%q", stored)
		} else {
			logf("[ERROR] registration conflict: stored=%q incoming=%q", stored, val)
			http.Error(
				w,
				fmt.Sprintf("[CONFLICT] already registered by %q, incoming=%q", stored, val),
				http.StatusConflict,
			)
			return
		}
	}

	// 选择 playbook
	playbook, warn, selErr := selectPlaybook(a.cfg.Ansible.PlaybookRoot, hostgroup)
	if warn != "" {
		logf("[WARN] %s", warn)
	}
	if selErr != nil {
		logf("[ERROR] %v", selErr)
		http.Error(w, "playbook select error: "+selErr.Error(), http.StatusNotFound)
		return
	}
	logf("[INFO] use playbook: %s", playbook)

	// 准备日志目录
	if err := os.MkdirAll(a.cfg.Ansible.LogDir, 0o755); err != nil {
		logf("[ERROR] mkdir log_dir failed: %v", err)
		http.Error(w, "mkdir log_dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 写 inventory 文件
	invBase := fmt.Sprintf("%s__%s__%s.txt", req.ID, req.Hostname, req.IP)
	invPath := filepath.Join(a.cfg.Ansible.LogDir, invBase)
	content := "[" + hostgroup + "]\n" + req.IP + "\n"

	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		logf("[ERROR] write inventory failed: %v", err)
		http.Error(w, "write inventory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logf("[INFO] inventory written: %s", invPath)

	// 步骤 1：设置主机名
	hostnameCmd := fmt.Sprintf(
		"ansible -u %s %s -i %s -m shell -a 'hostnamectl set-hostname %s'",
		a.cfg.Ansible.User, req.IP, invPath, req.Hostname,
	)

	if err := a.runAndStream(
		ctx,
		hostnameCmd,
		mustDur(a.cfg.Exec.HostnameTimeout, time.Minute),
		w,
		logf,
	); err != nil {
		logf("[ERROR] hostname step failed: %v", err)
		http.Error(w, "hostname step failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 步骤 2：执行 playbook 并 tee 到日志
	logFile := strings.TrimSuffix(invPath, ".txt") +
		"__" + time.Now().Format("2006-01-02_15:04:05.000000") + ".log"

	playbookCmd := fmt.Sprintf(
		"cd %s && ansible-playbook %s -i %s -e hosts=%s 2>&1 | tee %s",
		a.cfg.Ansible.PlaybookRoot, playbook, invPath, hostgroup, logFile,
	)

	if err := a.runAndStream(
		ctx,
		playbookCmd,
		mustDur(a.cfg.Exec.PlaybookTimeout, 2*time.Hour),
		w,
		logf,
	); err != nil {
		logf("[ERROR] playbook step failed: %v", err)
		http.Error(w, "playbook step failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logf("[INFO] initialize host done. log=%s", logFile)
}

// -------------------- 处理器：解除注册 --------------------

func (a *App) handleUnregistry(c *gin.Context) {
	if c.Request.Method != http.MethodPost {
		c.String(http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req HostReq
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20)).Decode(&req); err != nil {
		c.String(http.StatusBadRequest, "invalid json")
		return
	}
	if req.Hostname == "" {
		c.String(http.StatusBadRequest, "missing hostname")
		return
	}

	lockKey := "LOCK__" + req.Hostname
	ctx := c.Request.Context()

	// 如果提供了 ID/IP，就做守卫匹配
	if req.ID != "" || req.IP != "" {
		incoming := strings.Trim(req.ID+"__"+req.IP, "_")
		stored, _ := a.rdb.HGet(ctx, lockKey, "id__ip").Result()
		if stored != incoming {
			c.String(http.StatusPreconditionFailed, "mismatch: stored=%q incoming=%q", stored, incoming)
			return
		}
	}

	_, _ = a.rdb.Del(ctx, lockKey).Result()
	c.JSON(http.StatusOK, map[string]any{
		"ok":      true,
		"deleted": lockKey,
	})
}

// -------------------- playbook 选择 --------------------

// findPlaybookFile 在 root 下查找 base.{yml,yaml}，返回路径 + 是否存在
func findPlaybookFile(root, base string) (string, bool) {
	ymlPath := filepath.Join(root, base+".yml")
	yamlPath := filepath.Join(root, base+".yaml")

	ymlExists := fileExists(ymlPath)
	yamlExists := fileExists(yamlPath)

	// 策略：如果同时存在，优先 yml
	if ymlExists {
		return ymlPath, true
	}
	if yamlExists {
		return yamlPath, true
	}
	return "", false
}

func selectPlaybook(root, hostgroup string) (playbook string, warn string, err error) {
	def, defExists := findPlaybookFile(root, "default")
	hg, hgExists := findPlaybookFile(root, hostgroup)

	switch {
	case defExists && !hgExists:
		return def,
			fmt.Sprintf("hostgroup playbook missing: %s.{yml|yaml}; fallback to default", hostgroup),
			nil

	case !defExists && hgExists:
		return hg, "", nil

	case defExists && hgExists:
		return hg,
			"both default and hostgroup playbook exist; prefer hostgroup",
			nil

	default:
		return "",
			"",
			fmt.Errorf("no playbook found for hostgroup %s under %s (tried .yml/.yaml)", hostgroup, root)
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// -------------------- 命令执行 & 流式输出 --------------------

func (a *App) runAndStream(
	ctx context.Context,
	shellCmd string,
	timeout time.Duration,
	w http.ResponseWriter,
	logf func(string, ...any),
) error {
	logf("[INFO] run: %s", shellCmd)

	c := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		c, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(c, "/bin/bash", "-lc", shellCmd)

	if len(a.cfg.Exec.Env) > 0 {
		cmd.Env = append(os.Environ(), a.cfg.Exec.Env...)
	}

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
