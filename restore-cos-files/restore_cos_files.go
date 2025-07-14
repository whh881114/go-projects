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
	SecretID    string   `yaml:"secretId"`
	SecretKey   string   `yaml:"secretKey"`
	Region      string   `yaml:"region"`
	BucketName  string   `yaml:"bucketName"`
	Prefix      []string `yaml:"prefix"`
	MaxKeys     int      `yaml:"maxKeys"`
	RestoreDays int      `yaml:"restoreDays"`
	Workers     int      `yaml:"workers"`
}

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// 默认值设置
	dryRun := flag.Bool("dry-run", true, "仅打印将执行的操作，不执行恢复，默认为 true。")
	configPath := flag.String("config", ".cos-config.yaml", "指定配置文件路径，默认为 .cos-config.yaml。")
	date := flag.String("date", "", "指定日期，格式为 YYYY-MM-DD。此参数是必需的。")

	// 解析命令行参数
	flag.Parse()

	// 参数验证，确保有日期，并且无多余无关参数
	if *date == "" {
		fmt.Println("错误: 必须指定 --date 参数，格式为 YYYY-MM-DD。")
		printUsage()
		os.Exit(1)
	}

	if len(flag.Args()) > 0 {
		// 处理多余的噪音参数
		fmt.Println("警告: 存在无关参数：", flag.Args())
		printUsage()
		os.Exit(1)
	}

	// 加载配置文件
	cfg, err := loadConfig(*configPath)
	if err != nil {
		logrus.Fatalf("加载配置文件失败: %v", err)
	}

	// 输出 dry-run 状态
	if *dryRun {
		logrus.Info("启用 dry-run 模式，仅打印操作而不执行恢复")
	} else {
		logrus.Info("执行恢复操作，恢复到 Standard 状态")
	}

	// 创建 COS 客户端
	u, _ := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.BucketName, cfg.Region))
	b := &cos.BaseURL{BucketURL: u}

	// 创建一个新的 HTTP 客户端，并设置 AuthorizationTransport
	client := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  cfg.SecretID,  // 从配置文件读取 SecretID
			SecretKey: cfg.SecretKey, // 从配置文件读取 SecretKey
		},
	})

	// 创建一个字符串通道，用于在生产者和消费者之间传递文件名
	fileChan := make(chan string, 1000)
	var wg sync.WaitGroup // 使用 sync.WaitGroup 来等待所有 goroutine 完成

	// 生产者 goroutine：负责扫描并将文件对象推送到通道
	wg.Add(1)
	go func() {
		defer wg.Done() // 确保在 goroutine 完成时调用 Done()
		// 遍历配置文件中指定的 prefix，扫描并发送对象
		for _, prefix := range cfg.Prefix {
			scanAndSendObjects(client, cfg, prefix, *date, fileChan)
		}
		close(fileChan) // 扫描完成后关闭通道，通知消费者无更多数据
	}()

	// 消费者 goroutine：从通道中接收文件并执行恢复操作
	for i := 0; i < cfg.Workers; i++ { // 根据配置的工作线程数启动多个消费者
		wg.Add(1)
		go func(id int) {
			defer wg.Done() // 确保在 goroutine 完成时调用 Done()
			// 从通道中接收文件对象并恢复
			for key := range fileChan {
				restoreObject(client, key, cfg.RestoreDays, id, *dryRun)
			}
		}(i) // 启动消费者 goroutine，传递 id 作为标识
	}

	// 等待所有 goroutine 完成
	wg.Wait()

	// 打印日志，表示所有归档文件已处理完毕
	logrus.Info("所有归档文件处理完毕。")

}

func printUsage() {
	fmt.Println("\n用法示例:")
	fmt.Println("  ./restore-cos-files --date YYYY-MM-DD [--config xxxx.config] [--dry-run=false]")
	fmt.Println("\n参数说明:")
	fmt.Println("  --date       必需。指定日期，格式为 YYYY-MM-DD。")
	fmt.Println("  --config     可选。指定配置文件路径，默认为 .cos-config.yaml。")
	fmt.Println("  --dry-run    可选。默认为 true，表示仅打印操作而不执行恢复。如果要恢复数据，请指定 --dry-run=false。")
	fmt.Println("\n如果有无关的参数，将会显示警告并终止执行。")
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
		Prefix:    prefix,      // prefix 表示要查询的文件夹，其值中必须要带有'/'，例如"folder/"。
		Delimiter: "/",         // deliter 表示分隔符, 设置为/表示列出当前目录下的 object, 设置为空表示列出所有的 object
		MaxKeys:   cfg.MaxKeys, // 设置最大遍历出多少个对象, 一次 listobject 最大支持1000
	}

	var marker string
	isTruncated := true
	for isTruncated {
		opt.Marker = marker
		v, _, err := client.Bucket.Get(context.Background(), opt)
		if err != nil {
			logrus.Errorf("列举对象失败 (%s): %v", prefix, err)
			break
		}

		for _, content := range v.Contents {
			if strings.Contains(content.Key, date+"T") && content.StorageClass == "DEEP_ARCHIVE" {
				logrus.Infof("当前文件名：%s，其存储类型为：%s。", content.Key, content.StorageClass)
				out <- content.Key
			}
		}

		for _, commonPrefix := range v.CommonPrefixes {
			scanAndSendObjects(client, cfg, commonPrefix, date, out)
		}

		isTruncated = v.IsTruncated
		marker = v.NextMarker
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
