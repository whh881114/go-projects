package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// 配置文件结构
type RegistryConfig struct {
	Registries []RegistryEntry `yaml:"registries"`
}

type RegistryEntry struct {
	Name       string   `yaml:"name"`
	Server     string   `yaml:"server"`
	Username   string   `yaml:"username"`
	Password   string   `yaml:"password"`
	Namespaces []string `yaml:"namespaces"`
}

func main() {
	// 加载配置
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/etc/registry-config/config.yaml"
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 创建 k8s client
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("无法获取集群内配置: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		log.Fatalf("创建kubernetes client失败: %v", err)
	}

	ctx := context.Background()

	// 初始化时，把配置同步一遍
	if err := distributeSecrets(ctx, clientset, cfg); err != nil {
		log.Fatalf("初始化同步失败: %v", err)
	}

	// 监听 namespace 变化
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

	// 保持运行
	wait.NeverStop
	select {}

	stopCh := make(chan struct{})
	defer close(stopCh)

	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)

	// 保持 pod 存活
	select {}
}

func loadConfig(path string) (*RegistryConfig, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config RegistryConfig
	err = yaml.Unmarshal(data, &config)
	return &config, err
}

func distributeSecrets(ctx context.Context, clientset *kubernetes.Clientset, config *RegistryConfig) error {
	nsList, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("列出namespaces失败: %v", err)
	}

	nsMap := make(map[string]bool)
	for _, ns := range nsList.Items {
		nsMap[ns.Name] = true
	}

	for _, reg := range config.Registries {
		auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", reg.Username, reg.Password)))
		dockerCfg := map[string]interface{}{
			"auths": map[string]interface{}{
				reg.Server: map[string]string{
					"username": reg.Username,
					"password": reg.Password,
					"auth":     auth,
				},
			},
		}
		dockerCfgJson, _ := json.Marshal(dockerCfg)

		for _, ns := range reg.Namespaces {
			if !nsMap[ns] {
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
					corev1.DockerConfigJsonKey: dockerCfgJson,
				},
			}

			_, err := clientset.CoreV1().Secrets(ns).Get(ctx, reg.Name, metav1.GetOptions{})
			if err != nil {
				// 如果不存在就创建
				_, err = clientset.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
				if err != nil {
					log.Printf("在namespace %s 创建secret %s 失败: %v", ns, reg.Name, err)
				} else {
					log.Printf("在namespace %s 创建secret %s 成功", ns, reg.Name)
				}
			} else {
				// 存在就更新
				_, err = clientset.CoreV1().Secrets(ns).Update(ctx, secret, metav1.UpdateOptions{})
				if err != nil {
					log.Printf("在namespace %s 更新secret %s 失败: %v", ns, reg.Name, err)
				} else {
					log.Printf("在namespace %s 更新secret %s 成功", ns, reg.Name)
				}
			}
		}
	}

	return nil
}
