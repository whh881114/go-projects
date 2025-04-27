package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// SecretSpec 定义了需要创建的 pull-secret 及目标 Namespace
// 支持指定多个 Secret、多个 Namespace 或通配符*
type SecretSpec struct {
	Name       string   `yaml:"name"`       // Secret 名称
	Registry   string   `yaml:"registry"`   // Registry 地址
	Username   string   `yaml:"username"`   // 登录用户名
	Password   string   `yaml:"password"`   // 登录密码
	Namespaces []string `yaml:"namespaces"` // 目标 Namespace 列表，支持 "*" 表示所有
}

// Config 定义了整个分发规则
// 挂载 ConfigMap 后重启 Pod 会重新读取并分发一次
type Config struct {
	Secrets []SecretSpec `yaml:"secrets"`
}

func main() {
	// 获取配置文件路径
	configPath := flag.String("config", "/etc/registry-config/config.yaml", "Path to the distribution config YAML")
	flag.Parse()

	// 读取并解析 YAML
	yamlBytes, err := ioutil.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(yamlBytes, &cfg); err != nil {
		log.Fatalf("Failed to parse YAML: %v", err)
	}

	// 初始化 in-cluster Kubernetes 客户端
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to load in-cluster config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		log.Fatalf("Failed to create clientset: %v", err)
	}

	// 先对已有 Namespace 执行一次同步
	nsList, err := clientset.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		log.Fatalf("Failed to list namespaces: %v", err)
	}
	for _, ns := range nsList.Items {
		distributeSecretsToNamespace(clientset, cfg.Secrets, ns.Name)
	}
	log.Println("Initial secret distribution complete.")

	// 设置 Informer 实时监听 Namespace 增加事件
	factory := informers.NewSharedInformerFactory(clientset, 0)
	nsInformer := factory.Core().V1().Namespaces().Informer()
	nsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ns := obj.(*corev1.Namespace)
			log.Printf("Namespace added: %s, distributing secrets...", ns.Name)
			distributeSecretsToNamespace(clientset, cfg.Secrets, ns.Name)
		},
	})

	stopCh := make(chan struct{})
	go factory.Start(stopCh)

	// 等待 SIGTERM 或 SIGINT 退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	close(stopCh)
	log.Println("Shutting down secret distributor")
}

// distributeSecretsToNamespace 根据规则将所有 SecretSpec 分发到指定 Namespace
func distributeSecretsToNamespace(clientset *kubernetes.Clientset, specs []SecretSpec, namespace string) {
	for _, spec := range specs {
		if !matchesNamespace(spec.Namespaces, namespace) {
			continue
		}
		// 构造 Docker 配置 JSON
		dockerCfg := map[string]map[string]map[string]string{"auths": {
			spec.Registry: {
				"username": spec.Username,
				"password": spec.Password,
				"auth":     base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", spec.Username, spec.Password))),
			},
		}}
		jsonBytes, err := json.Marshal(dockerCfg)
		if err != nil {
			log.Printf("[%s][%s] JSON marshal error: %v", spec.Name, namespace, err)
			continue
		}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: spec.Name},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data:       map[string][]byte{".dockerconfigjson": jsonBytes},
		}

		// 尝试创建或更新 Secret
		_, err = clientset.CoreV1().Secrets(namespace).Get(spec.Name, metav1.GetOptions{})
		if err != nil {
			_, createErr := clientset.CoreV1().Secrets(namespace).Create(secret)
			if createErr != nil {
				log.Printf("[%s][%s] Create error: %v", spec.Name, namespace, createErr)
			} else {
				log.Printf("[%s][%s] Secret created", spec.Name, namespace)
			}
		} else {
			_, updateErr := clientset.CoreV1().Secrets(namespace).Update(secret)
			if updateErr != nil {
				log.Printf("[%s][%s] Update error: %v", spec.Name, namespace, updateErr)
			} else {
				log.Printf("[%s][%s] Secret updated", spec.Name, namespace)
			}
		}
	}
}

// matchesNamespace 判断当前 Namespace 是否在规则列表中（支持通配符）
func matchesNamespace(patterns []string, namespace string) bool {
	for _, p := range patterns {
		if p == "*" || p == namespace {
			return true
		}
	}
	return false
}
