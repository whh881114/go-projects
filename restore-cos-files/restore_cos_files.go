package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/tencentyun/cos-go-sdk-v5"
	"gopkg.in/yaml.v2"
)

type Config struct {
	SecretID     string   `yaml:"secretId"`
	SecretKey    string   `yaml:"secretKey"`
	SessionToken string   `yaml:"sessionToken"`
	Region       string   `yaml:"region"`
	BucketName   string   `yaml:"bucketName"`
	Prefix       []string `yaml:"prefix"`
	MaxKeys      int      `yaml:"maxKeys"`
	RestoreDays  int      `yaml:"restoreDays"`
	Workers      int      `yaml:"workers"`
}

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	dryRun := flag.Bool("dry-run", false, "仅打印将执行的操作，不执行恢复")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: ./restore-cos-files YYYY-MM-DD [.cos-config.yaml] [--dry-run]")
		os.Exit(1)
	}

	date := args[0]
	configPath := ".cos-config.yaml"
	if len(args) >= 2 {
		configPath = args[1]
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		logrus.Fatalf("加载配置文件失败: %v", err)
	}

	u, _ := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.BucketName, cfg.Region))
	b := &cos.BaseURL{BucketURL: u}
	client := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:     cfg.SecretID,
			SecretKey:    cfg.SecretKey,
			SessionToken: cfg.SessionToken,
		},
	})

	fileChan := make(chan string, 1000)
	var wg sync.WaitGroup

	// 生产者
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, prefix := range cfg.Prefix {
			scanAndSendObjects(client, cfg, prefix, date, fileChan)
		}
		close(fileChan)
	}()

	// 消费者
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for key := range fileChan {
				restoreObject(client, key, cfg.RestoreDays, id, *dryRun)
			}
		}(i)
	}

	wg.Wait()
	logrus.Info("所有归档文件处理完毕。")
}

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func scanAndSendObjects(client *cos.Client, cfg *Config, prefix, date string, out chan<- string) {
	opt := &cos.BucketGetOptions{
		Prefix:  prefix,
		MaxKeys: cfg.MaxKeys,
	}

	for {
		res, _, err := client.Bucket.Get(context.Background(), opt)
		if err != nil {
			logrus.Errorf("列举对象失败 (%s): %v", prefix, err)
			return
		}

		for _, obj := range res.Contents {
			if strings.Contains(obj.Key, date+"T") && obj.StorageClass == "DEEP_ARCHIVE" {
				out <- obj.Key
			}
		}

		if res.IsTruncated {
			opt.Marker = res.NextMarker
		} else {
			break
		}
	}
}

func restoreObject(client *cos.Client, key string, days, workerID int, dryRun bool) {
	if dryRun {
		logrus.Infof("[Worker %d] [dry-run] 将恢复: %s", workerID, key)
		return
	}

	opt := &cos.ObjectRestoreOptions{
		Days: days,
		Tier: &cos.CASJobParameters{
			Tier: "Standard", // 选择恢复类型
		},
	}

	_, err := client.Object.PostRestore(context.Background(), key, opt)
	if err != nil {
		logrus.Errorf("[Worker %d] 恢复失败: %s, 错误: %v", workerID, key, err)
	} else {
		logrus.Infof("[Worker %d] 恢复成功: %s", workerID, key)
	}
}
