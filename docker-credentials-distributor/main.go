package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// RegistryConfig 配置文件结构
type RegistryConfig struct {
	Registries []RegistryEntry `yaml:"registries"`
}

type RegistryEntry struct {
	Name       string   `yaml:"name"`
	Registry   string   `yaml:"registry"`
	Username   string   `yaml:"username"`
	Password   string   `yaml:"password"`
	Namespaces []string `yaml:"namespaces"`
}

func main() {
	// 使用 flag 解析命令行参数
	configFile := flag.String("config", "/etc/docker-credentials.yaml", "配置文件路径")
	flag.Parse()

	// 加载配置
	cfg, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	ctx := context.Background()

	// 创建 k8s client
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("无法获取集群内配置: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		log.Fatalf("创建kubernetes client失败: %v", err)
	}

	// LeaseLock 用来做 leader election
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      "secret-distributor-lock",
			Namespace: "default", // 放你部署的 namespace
		},
		Client: clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: os.Getenv("POD_NAME"), // 在 Pod Spec 里以 env 注入
		},
	}

	// 选举配置
	lec := leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,

		// 注意：是放在 Callbacks 里
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				log.Println("我当上 Leader 了，开始分发逻辑")
				runDistributor(ctx, clientset, cfg)
			},
			OnStoppedLeading: func() {
				log.Println("我不再是 Leader 了，进程退出")
				os.Exit(0)
			},
		},
	}

	// 启动选举（会阻塞，直到进程退出）
	leaderelection.RunOrDie(ctx, lec)

	// 初始化时，把配置同步一遍
	if err := distributeSecrets(ctx, clientset, cfg); err != nil {
		log.Fatalf("初始化同步失败: %v", err)
	}

	// 监听 namespace 变化，有更新时就会响应，如果每隔10分钟把全量列表同步一遍。
	factory := informers.NewSharedInformerFactory(clientset, time.Minute*10)
	nsInformer := factory.Core().V1().Namespaces().Informer()

	nsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ns := obj.(*corev1.Namespace)
			log.Printf("检测到新namespace: %s", ns.Name)
			err := distributeSecrets(ctx, clientset, cfg)
			if err != nil {
				log.Printf("分发secret失败: %v", err)
			}
		},
	})

	stopCh := make(chan struct{})
	defer close(stopCh)

	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)

	// 阻塞主线程，保持程序常驻
	select {}

}

func loadConfig(path string) (*RegistryConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg RegistryConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func distributeSecrets(ctx context.Context, clientset *kubernetes.Clientset, cfg *RegistryConfig) error {
	// 列出所有 namespace
	nsList, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("列出 namespaces 失败: %w", err)
	}
	// 快速查表
	existNS := map[string]bool{}
	for _, ns := range nsList.Items {
		existNS[ns.Name] = true
	}

	// 遍历每条 RegistryEntry
	for _, reg := range cfg.Registries {
		// 构造 .dockerconfigjson
		auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", reg.Username, reg.Password)))
		dockCfg := map[string]interface{}{
			"auths": map[string]interface{}{
				reg.Registry: map[string]string{
					"username": reg.Username,
					"password": reg.Password,
					"auth":     auth,
				},
			},
		}
		raw, _ := json.Marshal(dockCfg)

		for _, ns := range reg.Namespaces {
			if ns != "*" && !existNS[ns] {
				log.Printf("namespace %s 不存在，跳过", ns)
				continue
			}
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      reg.Name,
					Namespace: ns,
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: raw,
				},
			}

			// Create or Update
			if _, err := clientset.CoreV1().Secrets(ns).Get(ctx, reg.Name, metav1.GetOptions{}); err != nil {
				if _, err := clientset.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
					log.Printf("在 %s 创建 secret %s 失败: %v", ns, reg.Name, err)
				} else {
					log.Printf("在 %s 创建 secret %s 成功", ns, reg.Name)
				}
			} else {
				if _, err := clientset.CoreV1().Secrets(ns).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
					log.Printf("在 %s 更新 secret %s 失败: %v", ns, reg.Name, err)
				} else {
					log.Printf("在 %s 更新 secret %s 成功", ns, reg.Name)
				}
			}
		}
	}
	return nil
}

// runDistributor 负责“leader”真正干的活：一次全量 + informer 持续监听
func runDistributor(ctx context.Context, clientset *kubernetes.Clientset, cfg *RegistryConfig) {
	// 1) 初始化全量分发
	if err := distributeSecrets(ctx, clientset, cfg); err != nil {
		log.Fatalf("初始化分发失败: %v", err)
	}

	// 2) 启 informer 继续监听新 namespace
	factory := informers.NewSharedInformerFactory(clientset, 0)
	nsInf := factory.Core().V1().Namespaces().Informer()
	nsInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ns := obj.(*corev1.Namespace)
			log.Printf("Leader 分发模式：检测新 namespace %s", ns.Name)
			if err := distributeSecrets(ctx, clientset, cfg); err != nil {
				log.Printf("分发失败: %v", err)
			}
		},
	})

	stopCh := make(chan struct{})
	defer close(stopCh)
	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)

	// 阻塞直到 Context 被取消（比如 OnStoppedLeading 调用 os.Exit）
	<-ctx.Done()
}
